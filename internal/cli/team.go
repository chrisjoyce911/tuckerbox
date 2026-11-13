package cli

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/team"
)

func (a App) teamClient(ctx context.Context, cfg *config.Config) (forge.TeamClient, error) {
	connect := a.Forge
	if connect == nil {
		connect = func(ctx context.Context, cfg *config.Config) (forge.Client, func(string) string, error) {
			return ship.Connect(ctx, cfg, nil)
		}
	}
	c, mask, err := connect(ctx, cfg)
	if err != nil {
		if mask != nil {
			return nil, fmt.Errorf("%s", mask(err.Error()))
		}
		return nil, err
	}
	tc, ok := c.(forge.TeamClient)
	if !ok {
		return nil, fmt.Errorf("forge does not support team mode")
	}
	return tc, nil
}
func (a App) teamBase(ctx context.Context, repo runs.Repo, cfg *config.Config) (string, error) {
	c, err := a.teamClient(ctx, cfg)
	if err != nil {
		return "", err
	}
	rev, _, err := team.Base(ctx, repo.Root, cfg, c)
	return rev, err
}
func (a App) teamGate(ctx context.Context, repo runs.Repo, cfg *config.Config, change string, sp runs.StartPoint, resume bool) error {
	rev, err := a.teamBase(ctx, repo, cfg)
	if err != nil {
		return err
	}
	if !cfg.RequireApproval {
		return nil
	}
	var actual approval.Files
	if sp.Worktree != "" {
		actual = approval.DirOf(sp.Worktree, change)
	} else {
		actual, err = approval.NewRev(ctx, repo.Root, sp.Rev, change)
		if err != nil {
			return err
		}
	}
	_, err = team.Judge(ctx, repo.Root, rev, change, actual, resume)
	return err
}
func (a App) teamSettingsGate(ctx context.Context, repo runs.Repo, cfg *config.Config, change, worktree string) error {
	return a.teamGate(ctx, repo, cfg, change, runs.StartPoint{Worktree: worktree}, true)
}
