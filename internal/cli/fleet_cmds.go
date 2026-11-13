package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

func (a App) claimCmd(args []string) int {
	if len(args) == 0 {
		return a.usageErr("claim", "usage: tuckerbox claim show [--json] | release <change> [--force]")
	}
	var asJSON, force bool
	pos, err := parseArgs(args[1:], map[string]*bool{"json": &asJSON, "force": &force}, nil)
	if err != nil {
		return a.usageErr("claim", err.Error())
	}
	if args[0] != "show" && args[0] != "release" || args[0] == "show" && (len(pos) != 0 || force) || args[0] == "release" && (len(pos) != 1 || asJSON) {
		return a.usageErr("claim", "usage: tuckerbox claim show [--json] | release <change> [--force]")
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("claim", "%v", err)
	}
	repo, cfg, ok := a.loadRepo("claim", wd)
	if !ok {
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := fleet.Store{Dir: repo.Root, Remote: cfg.Shipping.Remote, Lease: cfg.Fleet.Lease, MaxRuns: cfg.Fleet.MaxRuns}
	if args[0] == "show" {
		claims, err := store.Read(ctx)
		if err != nil {
			return a.errf("claim", "%v", err)
		}
		list := make([]fleet.Claim, 0, len(claims))
		for _, c := range claims {
			c.State = c.Effective(cfg.Fleet.Lease, time.Now())
			list = append(list, c)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Change < list[j].Change })
		if asJSON {
			if err := json.NewEncoder(a.Stdout).Encode(list); err != nil {
				return a.errf("claim", "%v", err)
			}
		} else {
			for _, c := range list {
				fmt.Fprintf(a.Stdout, "%s\t%s\t%s\tepoch %d\trenewed %s\n", c.Change, c.Machine, c.State, c.Epoch, c.RenewedAt.In(brisbane()).Format(time.RFC3339))
			}
		}
		return 0
	}
	change := pos[0]
	if err := statedir.CheckID("change", change); err != nil {
		return a.errf("claim", "%v", err)
	}
	machine, err := ship.MachineName(cfg.Machine)
	if err != nil {
		return a.errf("claim", "%v", err)
	}
	// The same change lock used by run prevents a local release racing a
	// continuation; foreign --force intentionally fences an active holder.
	path := repo.WorktreePath(cfg.Worktree, change)
	ws, werr := runs.OpenWorkspace(ctx, path)
	if werr == nil {
		lock, err := runs.TryChangeLock(ws.State.RunLock())
		if err != nil {
			return a.errf("claim", "cannot release an active local run: %v", err)
		}
		defer lock.Release()
	}
	store.Base, _ = repo.Base(ctx, cfg.Worktree.Base, nil)
	if err := store.Release(ctx, change, machine, repo.BranchName(cfg.Worktree, change), force); err != nil {
		return a.errf("claim", "%v", err)
	}
	fmt.Fprintf(a.Stdout, "%s: claim released\n", change)
	return 0
}
func brisbane() *time.Location {
	loc, err := time.LoadLocation("Australia/Brisbane")
	if err != nil {
		return time.FixedZone("AEST", 10*60*60)
	}
	return loc
}
func (a App) fleetDoctorLine() string {
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return ""
	}
	opts, err := config.Discover(env, wd)
	if err != nil {
		return ""
	}
	cfg, err := config.Load(opts)
	if err != nil || cfg.Mode != config.ModeFleet {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := (fleet.Store{Dir: opts.RepoRoot, Remote: cfg.Shipping.Remote}).Read(ctx); err != nil {
		return "fleet: cannot fetch " + fleet.RefName + ": " + err.Error()
	}
	return "fleet: " + fleet.RefName + " can be fetched"
}
