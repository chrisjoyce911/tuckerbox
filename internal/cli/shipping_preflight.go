package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
)

func (a App) shippingPreflight(ctx context.Context, repo runs.Repo, cfg *config.Config, changes []string) bool {
	if cfg.Ship == config.ShipLocal {
		return true
	}
	connect := a.Forge
	if connect == nil {
		connect = func(ctx context.Context, cfg *config.Config) (forge.Client, func(string) string, error) {
			return ship.Connect(ctx, cfg, nil)
		}
	}
	client, mask, err := connect(ctx, cfg)
	if mask == nil {
		mask = func(s string) string { return s }
	}
	if err != nil {
		a.errf("run", "%s", mask(err.Error()))
		return false
	}
	machine, err := ship.MachineName(cfg.Machine)
	if err != nil {
		a.errf("run", "%v", err)
		return false
	}
	for _, id := range changes {
		err := runs.ShipPreflight(ctx, client, machine, repo.BranchName(cfg.Worktree, id))
		if err == nil {
			continue
		}
		if strings.HasPrefix(err.Error(), "claimed by ") {
			a.errf("run", "%s: %s", id, mask(err.Error()))
			return false
		}
		if api, ok := err.(*forge.APIError); ok && (api.Status == 401 || api.Status == 403) {
			a.errf("run", "%s", mask(err.Error()))
			return false
		}
		fmt.Fprintf(a.Stderr, "warning: forge unreachable for %s; starting anyway: %s\n", id, mask(err.Error()))
	}
	return true
}
