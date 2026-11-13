package runs

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"strconv"
)

// teamStart is called with the change lock, before any agent starts. A scope
// continuation checks its existing claim without pushing or making a commit.
func (e *Engine) teamStart(ctx context.Context, ws Workspace, rs *Run) error {
	if e.Config.Mode != config.ModeTeam && rs.TeamLogin == "" {
		return nil
	}
	client, mask, err := e.shippingClient(ctx)
	if err != nil {
		return err
	}
	tc, ok := client.(forge.TeamClient)
	if !ok {
		return fmt.Errorf("forge does not support team mode")
	}
	login, err := tc.Login(ctx)
	if err != nil {
		return fmt.Errorf("%s", mask(err.Error()))
	}
	machine, err := ship.MachineName(e.Config.Machine)
	if err != nil {
		return err
	}
	if rs.TeamLogin != "" && rs.TeamLogin != login {
		return fmt.Errorf("claimed by %s", rs.TeamLogin)
	}
	id := ship.Identity{Change: rs.Change, Machine: machine, Run: strconv.FormatInt(rs.Created.UnixNano(), 10), By: login}
	access, err := client.Access(ctx)
	if err != nil {
		return err
	}
	g := ship.Git{Worktree: ws.Path, Remote: e.Config.Shipping.Remote, Branch: rs.Branch, Base: baseName(rs.Base, e.Repo.remote(ctx)), DefaultBranch: access.DefaultBranch}
	claim := ship.Claim{Identity: id, Git: g, Lease: rs.LastPush}
	if rs.MR != nil {
		if err := claim.Check(ctx, tc); err != nil {
			return err
		}
		return nil
	}
	// Detect foreign MRs or leftover branches before adding a start commit.
	if mr, err := tc.FindMR(ctx, rs.Branch); err != nil {
		return err
	} else if mr != nil && rs.LastPush == "" {
		return fmt.Errorf("claimed by %s on %s: %s", mr.Author, claimMachine(mr), mr.URL)
	}
	if rs.LastPush == "" {
		tip, err := g.Tip(ctx)
		if err != nil {
			return err
		}
		if tip != "" {
			return fmt.Errorf("remote branch exists")
		}
		if _, err := git(ctx, ws.Path, "commit", "--allow-empty", "-m", "Start "+rs.Change+"\n\n[skip ci]"); err != nil {
			return err
		}
	}
	rs.TeamLogin = login
	mr, err := claim.Acquire(ctx, tc, forge.OpenRequest{Title: rs.Change, Body: "Work in progress.", Draft: true}, func(sha string) error { rs.LastPush = sha; return e.save(ws, rs) })
	if err != nil {
		return fmt.Errorf("%s", mask(err.Error()))
	}
	rs.MR = &ship.MRInfo{Link: mr.URL, Number: mr.Number, State: mr.State}
	return e.save(ws, rs)
}
func claimMachine(mr *forge.MR) string { id, _ := ship.ParseMarker(mr.Body); return id.Machine }
