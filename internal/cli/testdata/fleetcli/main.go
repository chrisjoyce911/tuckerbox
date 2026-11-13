// fleetcli runs the production CLI with a local, shared fake forge. Detached
// engines re-execute this binary, keeping the same injected forge client.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/cli"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv/fleetforge"
)

func main() {
	client := fleetforge.Client{Dir: os.Getenv("FLEET_E2E_FORGE")}
	if len(os.Args) > 1 && os.Args[1] == "fleet-test-effect" {
		if len(os.Args) != 4 {
			panic("effect kind and saved run required")
		}
		r, err := runs.ReadRun(os.Args[3])
		if err == nil {
			if r.FleetClaim == nil {
				panic("saved run has no fleet claim")
			}
			fence := &fleet.Fence{Store: fleet.Store{Dir: r.Repo, Remote: r.FleetRemote}, Claim: *r.FleetClaim}
			switch os.Args[2] {
			case "renew":
				err = fence.Renew(context.Background())
			case "push":
				g := ship.Git{Worktree: r.Worktree, Remote: r.FleetRemote, Branch: r.Branch, Base: r.Base, Fleet: fence}
				_, err = g.Push(context.Background(), r.LastPush)
			case "merge":
				err = staleMerge(client, r)
			default:
				panic("unknown effect")
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	app := cli.App{Stdout: os.Stdout, Stderr: os.Stderr, DoctorEnv: doctor.SystemEnv(), DoctorTools: doctor.DefaultTools, Forge: func(context.Context, *config.Config) (forge.Client, func(string) string, error) {
		return client, func(s string) string { return s }, nil
	}}
	os.Exit(app.Run(os.Args[1:]))
}

// mergeWaitForge freezes one already-green observation. The normal CLI engines
// use Client directly, so a takeover can run while this stale attempt is paused.
type mergeWaitForge struct{ fleetforge.Client }

func (c mergeWaitForge) Checks(ctx context.Context, sha, base string) (forge.Checks, error) {
	checks, err := c.Client.Checks(ctx, sha, base)
	if err != nil {
		return checks, err
	}
	if err = os.WriteFile(filepath.Join(c.Dir, "merge-seen-green"), []byte(sha), 0600); err != nil {
		return checks, err
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(filepath.Join(c.Dir, "merge-continue")); err == nil {
			return checks, nil
		} else if !os.IsNotExist(err) {
			return checks, err
		}
		select {
		case <-ctx.Done():
			return checks, ctx.Err()
		case <-tick.C:
		}
	}
}

// Run the production ship.Engine all the way into watch.go's ShipMerge case.
// Startup still owns the saved claim; it is lost only while Checks is paused.
func staleMerge(client fleetforge.Client, r *runs.Run) error {
	// The parent owns this process and kills it in test cleanup. Use its
	// deadline: a separate short wall clock can expire while a slow Windows
	// runner is still performing the deliberate takeover between our gates.
	ctx, cancel := context.WithCancel(context.Background())
	if value := os.Getenv("FLEET_E2E_DEADLINE"); value != "" {
		deadline, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			cancel()
			return err
		}
		cancel()
		ctx, cancel = context.WithDeadline(context.Background(), deadline)
	}
	defer cancel()
	opts, err := config.Discover(config.SystemEnv(), r.Repo)
	if err != nil {
		return err
	}
	cfg, err := config.Load(opts)
	if err != nil {
		return err
	}
	cfg.Ship = config.ShipMerge
	fence := &fleet.Fence{Store: fleet.Store{Dir: r.Repo, Remote: r.FleetRemote}, Claim: *r.FleetClaim}
	stage := ship.Engine{Config: cfg, Git: ship.Git{Worktree: r.Worktree, Remote: r.FleetRemote, Branch: r.Branch, Base: r.Base, Fleet: fence}, Identity: ship.Identity{Change: r.Change, Machine: r.FleetClaim.Machine, Run: r.FleetClaim.Run, Epoch: r.FleetClaim.Epoch}, Forge: mergeWaitForge{client}, Record: &r.Record, Request: forge.OpenRequest{Title: r.Change, Branch: r.Branch, Base: strings.TrimPrefix(r.Base, "origin/")}, Save: func() error { return nil }, Commit: func(ctx context.Context, msg string) (bool, error) { return runs.CommitAll(ctx, r.Worktree, msg) }}
	out := stage.Run(ctx, true)
	if out.State != "merged" {
		return fmt.Errorf("%s: %s", out.State, out.Reason)
	}
	return nil
}
