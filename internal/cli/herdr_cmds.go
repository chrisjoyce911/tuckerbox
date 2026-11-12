package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/herdrmod"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

// herdr: tuckerbox herdr install
func (a App) herdr(args []string) int {
	if len(args) != 1 || args[0] != "install" {
		return a.usageErr("herdr", "usage: tuckerbox herdr install")
	}
	info := version.Get()
	res, err := herdrmod.Install(context.Background(), a.HerdrEnv, info.Version, info.Commit)
	var ce *herdrmod.CheckError
	switch {
	case errors.Is(err, herdrmod.ErrNoHerdr):
		return a.errf("herdr install", "%v; install the Herdr app and open it once, then run this again. Nothing was written", err)
	case errors.As(err, &ce):
		fmt.Fprint(a.Stderr, ce.Output)
		return a.errf("herdr install", "herdr-app check rejected the board mod; nothing was installed or changed")
	case err != nil:
		return a.errf("herdr install", "%v", err)
	}
	verb := "installed"
	if res.Updated {
		verb = "updated"
	}
	fmt.Fprintf(a.Stdout, "%s the board mod %s in %s\n", verb, info.Version, res.Dir)
	if res.Checked {
		fmt.Fprintln(a.Stdout, "herdr-app check passed")
	} else {
		fmt.Fprintln(a.Stdout, "herdr-app not found, so the mod wasn't checked; Herdr loads it when it next starts or reloads mods")
	}
	return 0
}

// herdrDoctorLine is doctor's line about the board mod, or "" when Herdr
// isn't installed. It never fails doctor: the board is optional.
func (a App) herdrDoctorLine() string {
	s, err := herdrmod.Inspect(a.HerdrEnv)
	switch {
	case err != nil:
		return fmt.Sprintf("herdr board mod: can't check (%v)", err)
	case !s.Herdr:
		return ""
	case !s.Installed:
		return "herdr board mod: not installed (tuckerbox herdr install)"
	case len(s.Modified) > 0 || len(s.Missing) > 0:
		var what []string
		for _, f := range s.Modified {
			what = append(what, f+" changed")
		}
		for _, f := range s.Missing {
			what = append(what, f+" missing")
		}
		return fmt.Sprintf("warning: herdr board mod in %s differs from what this tuckerbox ships (%s): run tuckerbox herdr install", s.Dir, strings.Join(what, ", "))
	case !s.Current:
		was := s.Stamp.Version
		if was == "" {
			was = "unknown version"
		}
		return fmt.Sprintf("warning: herdr board mod (%s) is not the one this tuckerbox (%s) ships: run tuckerbox herdr install", was, version.Get().Version)
	default:
		return fmt.Sprintf("herdr board mod: %s, current", s.Stamp.Version)
	}
}
