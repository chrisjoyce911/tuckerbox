package config

import (
	"errors"
	"io/fs"
	"os"
	"maps"
	"slices"
	"strings"
	"time"
)

// resolveModes fills in Mode, Fleet and Team from repository config and
// FleetCapacity and Machines from machine config. checkScopes has already
// dropped (and reported) any of them set in the wrong file.
func resolveModes(cfg *Config, m, r *file, add addFunc) {
	mr, rr := &m.raw, &r.raw

	cfg.Mode = ModeSolo
	if rr.Mode != nil {
		cfg.Mode = Mode(*rr.Mode)
		switch cfg.Mode {
		case ModeSolo, ModeFleet, ModeTeam:
		default:
			add(r.path, "mode", "unknown mode %q; use \"solo\", \"fleet\" or \"team\"", *rr.Mode)
		}
	}

	cfg.Fleet = Fleet{Lease: DefaultFleetLease}
	if v := rr.Fleet.MaxRuns; v != nil {
		if *v < 1 {
			add(r.path, "fleet.max_runs", "must be at least 1, got %d; leave it out for no limit", *v)
		} else {
			cfg.Fleet.MaxRuns = *v
		}
	}
	if v := rr.Fleet.Lease; v != nil {
		d, err := time.ParseDuration(*v)
		switch {
		case err != nil:
			add(r.path, "fleet.lease", "%q is not a duration such as \"10m\"", *v)
		case d < MinFleetLease:
			add(r.path, "fleet.lease", "%s is too short; use at least %s", *v, MinFleetLease)
		default:
			cfg.Fleet.Lease = d
		}
	}

	for _, login := range rr.Team.Approvers {
		if strings.TrimSpace(login) == "" || strings.ContainsAny(login, " \t\r\n/") {
			add(r.path, "team.approvers", "%q is not a forge login", login)
			continue
		}
		cfg.Team.Approvers = append(cfg.Team.Approvers, login)
	}

	switch cfg.Mode {
	case ModeTeam:
		if cfg.Ship != ShipMR && cfg.Ship != ShipMerge {
			add(r.path, "mode", "team mode needs ship = \"mr\" or \"merge\": a team claims a change with a merge request on the forge")
		}
		if cfg.RequireApproval && len(cfg.Team.Approvers) == 0 {
			add(r.path, "team.approvers", "team mode with require_approval needs approvers; list the forge logins whose approval counts")
		}
	}

	if v := mr.FleetCapacity; v != nil {
		if *v < 0 {
			add(m.path, "fleet_capacity", "must be 0 or more, got %d", *v)
		} else {
			cfg.FleetCapacity = *v
		}
	}

	cfg.Machines = map[string]MachineEntry{}
	for _, name := range slices.Sorted(maps.Keys(mr.Machines)) {
		raw := mr.Machines[name]
		key := "machines." + name
		e := MachineEntry{Name: name}
		if raw.SSH != nil {
			e.SSH = *raw.SSH
		}
		if raw.HC != nil {
			e.HC = *raw.HC
		}
		switch {
		case !nameRE.MatchString(name):
			add(m.path, key, "invalid machine name %q", name)
			continue
		case e.SSH != "" && e.HC != "":
			add(m.path, key, "give one of ssh or hc, not both")
			continue
		case e.SSH == "" && e.HC == "":
			add(m.path, key, "missing; give ssh = \"<destination>\" or hc = \"<herdr-core machine>\"")
			continue
		case strings.HasPrefix(e.SSH, "-") || strings.ContainsAny(e.SSH, " \t\r\n"):
			add(m.path, key+".ssh", "%q is not an SSH destination", e.SSH)
			continue
		case strings.HasPrefix(e.HC, "-") || strings.ContainsAny(e.HC, " \t\r\n"):
			add(m.path, key+".hc", "%q is not a herdr-core machine name", e.HC)
			continue
		}
		cfg.Machines[name] = e
	}
}

// fleetRemoteProblem checks that a fleet repository has the remote that
// holds its fleet ref: [shipping] remote, else origin.
func fleetRemoteProblem(opts Options, cfg *Config, repoPath string) (Problem, bool) {
	name := cfg.Shipping.Remote
	if name == "" {
		name = "origin"
	}
	var err error
	if opts.PushURL != nil {
		var u string
		u, err = opts.PushURL(name)
		if err == nil && u == "" {
			err = errNoURL
		}
	} else if opts.Remote == "" {
		err = errNoURL
	}
	if err == nil {
		return Problem{}, true
	}
	return Problem{File: repoPath, Key: "mode", Message: "fleet mode needs a remote to hold refs/tuckerbox/fleet; add remote " + name + " or set [shipping] remote"}, false
}

var errNoURL = errors.New("no remote URL")

// LoadMachine reads only the machine config at path, for commands that run
// outside a repository (tuckerbox machine --json, doctor). The result holds
// the machine's settings: Machine, FleetCapacity, Machines, Agents and
// Permissions; repository settings keep their defaults. A missing file is
// an empty machine config. Problems are those of the machine file only.
func LoadMachine(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = []byte{}
	case err != nil:
		return nil, err
	}
	m := parse(path, string(data), false)
	if m.broken {
		return &Config{MachinePath: path, Machines: map[string]MachineEntry{}}, m.problems
	}
	r := &file{path: "", repo: true, defined: func(...string) bool { return false }}
	cfg, problems := resolve("", "", m, r)
	cfg.MachinePath = path
	var mine Problems
	for _, p := range problems {
		if p.File == path {
			mine = append(mine, p)
		}
	}
	if len(mine) > 0 {
		return cfg, mine
	}
	return cfg, nil
}
