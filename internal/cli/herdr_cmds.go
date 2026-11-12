package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/herdrmod"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

const herdrUsage = "usage: tuckerbox herdr install [--keys [--key <key>]] [--drawer] | tuckerbox herdr uninstall"

// herdr: tuckerbox herdr install [--keys [--key <key>]] [--drawer], tuckerbox herdr uninstall
func (a App) herdr(args []string) int {
	var keys, drawer bool
	var key string
	pos, err := parseArgs(args, map[string]*bool{"keys": &keys, "drawer": &drawer}, map[string]*string{"key": &key})
	if err != nil || len(pos) != 1 {
		return a.usageErr("herdr", herdrUsage)
	}
	switch pos[0] {
	case "install":
		if key != "" && !keys {
			return a.usageErr("herdr", "--key needs --keys")
		}
		return a.herdrInstall(herdrmod.Options{Keys: keys, Key: key, Drawer: drawer})
	case "uninstall":
		if keys || drawer || key != "" {
			return a.usageErr("herdr", "uninstall takes no flags: it undoes what install recorded")
		}
		return a.herdrUninstall()
	}
	return a.usageErr("herdr", herdrUsage)
}

func (a App) herdrInstall(opt herdrmod.Options) int {
	info := version.Get()
	ctx := context.Background()
	res, err := herdrmod.Install(ctx, a.HerdrEnv, info.Version, info.Commit)
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
	did, err := herdrmod.InstallExtras(ctx, a.HerdrEnv, opt)
	for _, d := range did {
		fmt.Fprintln(a.Stdout, d)
	}
	if err != nil {
		return a.errf("herdr install", "%v", err)
	}
	return 0
}

func (a App) herdrUninstall() int {
	did, err := herdrmod.Uninstall(context.Background(), a.HerdrEnv)
	for _, d := range did {
		fmt.Fprintln(a.Stdout, d)
	}
	switch {
	case errors.Is(err, herdrmod.ErrNoHerdr), errors.Is(err, herdrmod.ErrNothingInstalled):
		return a.errf("herdr uninstall", "%v; nothing was changed", err)
	case err != nil:
		return a.errf("herdr uninstall", "%v", err)
	}
	fmt.Fprintln(a.Stdout, "uninstalled; close any board panes you docked, they have no mod now")
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
