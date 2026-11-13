// fleetcli runs the production CLI with a local, shared fake forge. Detached
// engines re-execute this binary, keeping the same injected forge client.
package main

import (
	"context"
	"fmt"
	"os"

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
				err = client.Merge(context.Background(), r.MR.Number, r.LastPush, "merge")
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
