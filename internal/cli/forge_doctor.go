package cli

import (
	"context"
	"errors"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
)

func (a App) forgeDoctorLine() string {
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
	if errors.Is(err, config.ErrNoRepoConfig) {
		return ""
	}
	if err != nil {
		return "forge: cannot check repository config: " + err.Error()
	}
	if cfg.Ship == config.ShipLocal {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return doctor.CheckForge(ctx, cfg.ForgeTarget, cfg.Forges[cfg.ForgeTarget.Host].Credentials(), nil)
}
