package runs

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
)

func (e *Engine) forgeShip(ctx context.Context, ws Workspace, rs *Run, resume bool) (*Run, error) {
	rs.State, rs.Step, rs.Reason = Shipping, "", ""
	if !rs.ShippingStarted {
		rs.ShippingStarted = true
		rs.ShipRound = 1
	}
	if err := e.save(ws, rs); err != nil {
		return rs, err
	}
	if e.workflowSlot != nil {
		if err := e.workflowSlot.Release(); err != nil {
			return e.finish(ws, rs, Blocked, err.Error())
		}
	}
	client, mask, err := e.shippingClient(ctx)
	if err != nil {
		return e.finish(ws, rs, Blocked, err.Error())
	}
	access, err := client.Access(ctx)
	if err != nil {
		return e.finish(ws, rs, Blocked, mask(err.Error()))
	}
	machine, err := ship.MachineName(e.Config.Machine)
	if err != nil {
		return e.finish(ws, rs, Blocked, err.Error())
	}
	g := ship.Git{Worktree: ws.Path, Remote: e.Config.Shipping.Remote, Branch: rs.Branch, Base: baseName(rs.Base, e.Repo.remote(ctx)), DefaultBranch: access.DefaultBranch}
	g.Fleet = e.fleetFence(rs)
	stage := ship.Engine{LogDir: string(ws.State), Fix: func(ctx context.Context, step string, findings []agent.Finding) (*agent.Result, error) {
		return e.shippingFix(ctx, ws, rs, step, findings)
	}, Config: e.Config, Git: g, Identity: ship.Identity{Change: rs.Change, Machine: machine, By: rs.TeamLogin, Epoch: e.fleetEpoch(rs), Run: strconv.FormatInt(rs.Created.UnixNano(), 10)}, Forge: client, Record: &rs.Record, Now: e.Now, Mask: mask, Sleep: e.ShipSleep,
		Save: func() error { return e.save(ws, rs) }, Commit: func(ctx context.Context, msg string) (bool, error) { return CommitAll(ctx, ws.Path, msg) },
		Event: func(ev ship.Event) {
			e.event(ws, rs, history.Event{Kind: history.Kind(ev.Kind), Commit: ev.Commit, Branch: ev.Branch, Link: ev.Link, Number: ev.Number, Method: ev.Method, ShipRound: ev.Round, CI: ev.CI, Jobs: ev.Jobs, Threads: ev.Threads})
		},
		ReleaseSlot: func() error {
			if e.workflowSlot != nil {
				return e.workflowSlot.Release()
			}
			return nil
		},
	}
	out := stage.Run(ctx, resume)
	if out.State == "ready" {
		rs.Complete = true
	}
	return e.finish(ws, rs, State(out.State), out.Reason)
}
func (e *Engine) shippingClient(ctx context.Context) (forge.Client, func(string) string, error) {
	if e.Forge != nil {
		client, mask, err := e.Forge(ctx)
		if mask == nil {
			mask = func(s string) string { return s }
		}
		return client, mask, err
	}
	return ship.Connect(ctx, e.Config, nil)
}

// ShipPreflight is advisory about an unavailable forge, but definitive about
// a missing token and a foreign open claim. It never creates or updates refs.
func ShipPreflight(ctx context.Context, cfgClient forge.Client, machine, branch string) error {
	mr, err := cfgClient.FindMR(ctx, branch)
	if err != nil {
		return err
	}
	if mr == nil {
		return nil
	}
	claim, ok := ship.ParseMarker(mr.Body)
	if !ok {
		return fmt.Errorf("claimed by another owner: %s", mr.URL)
	}
	if claim.Machine != machine {
		return fmt.Errorf("claimed by %s: %s", claim.Machine, mr.URL)
	}
	return nil
}
func baseName(base, remote string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(base, "refs/heads/"), "refs/remotes/"), remote+"/")
}
