package config

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Scenario: Default mode. A repository that does not set mode runs solo,
// with the default fleet lease and no limit.
func TestDefaultMode(t *testing.T) {
	cfg := newFixture(t, minimal, "").mustLoad()
	if cfg.Mode != ModeSolo {
		t.Fatalf("mode = %q, want solo", cfg.Mode)
	}
	if cfg.Fleet.MaxRuns != 0 || cfg.Fleet.Lease != DefaultFleetLease {
		t.Fatalf("fleet = %+v", cfg.Fleet)
	}
	if cfg.FleetCapacity != 0 || len(cfg.Machines) != 0 {
		t.Fatalf("capacity %d machines %v", cfg.FleetCapacity, cfg.Machines)
	}
}

// Scenario: Machine config sets the mode.
func TestMachineConfigSetsMode(t *testing.T) {
	f := newFixture(t, minimal, "mode = \"fleet\"\n")
	wantProblem(t, f.problems(), f.machine, "mode", "belongs in repository config")
}

// Scenario: Machine config sets a fleet limit.
func TestMachineConfigSetsFleetLimit(t *testing.T) {
	f := newFixture(t, minimal, "[fleet]\nmax_runs = 10\n")
	ps := f.problems()
	wantProblem(t, ps, f.machine, "fleet.max_runs", "belongs in repository config")
	if !strings.Contains(ps.Error(), "fleet.max_runs") {
		t.Fatalf("problems do not name fleet.max_runs:\n%v", ps)
	}
}

func TestMachineConfigSetsTeam(t *testing.T) {
	f := newFixture(t, minimal, "[team]\napprovers = [\"sam\"]\n")
	wantProblem(t, f.problems(), f.machine, "team.approvers", "belongs in repository config")
}

// Scenario: Repository lists machines.
func TestRepositoryListsMachines(t *testing.T) {
	f := newFixture(t, minimal+"\n[machines.mini]\nssh = \"mini\"\n", "")
	wantProblem(t, f.problems(), f.repoPath(), "machines", "belongs in machine config")
}

func TestRepositorySetsFleetCapacity(t *testing.T) {
	f := newFixture(t, "fleet_capacity = 2\n"+minimal, "")
	wantProblem(t, f.problems(), f.repoPath(), "fleet_capacity", "belongs in machine config")
}

func TestModesResolved(t *testing.T) {
	repo := "mode = \"fleet\"\n" + minimal + "\n[fleet]\nmax_runs = 4\nlease = \"5m\"\n\n[team]\napprovers = [\"sam\", \"alex\"]\n"
	machine := "fleet_capacity = 2\n\n[machines.mini]\nssh = \"mini\"\n\n[machines.lab]\nhc = \"lab\"\n"
	f := newFixture(t, repo, machine)
	f.remote = "git@github.com:sam/app.git"
	cfg := f.mustLoad()
	if cfg.Mode != ModeFleet || cfg.Fleet.MaxRuns != 4 || cfg.Fleet.Lease != 5*time.Minute {
		t.Fatalf("mode %q fleet %+v", cfg.Mode, cfg.Fleet)
	}
	if !slices.Equal(cfg.Team.Approvers, []string{"sam", "alex"}) {
		t.Fatalf("approvers %v", cfg.Team.Approvers)
	}
	if cfg.FleetCapacity != 2 {
		t.Fatalf("capacity %d", cfg.FleetCapacity)
	}
	want := map[string]MachineEntry{"mini": {Name: "mini", SSH: "mini"}, "lab": {Name: "lab", HC: "lab"}}
	if len(cfg.Machines) != 2 || cfg.Machines["mini"] != want["mini"] || cfg.Machines["lab"] != want["lab"] {
		t.Fatalf("machines %+v", cfg.Machines)
	}
	if cfg.Machines["lab"].Via() != "hc" || cfg.Machines["mini"].Via() != "ssh" {
		t.Fatal("Via")
	}
}

func TestModeValidation(t *testing.T) {
	cases := []struct {
		name, repo, machine, file, key, frag string
	}{
		{"unknown mode", "mode = \"swarm\"\n", "", "repo", "mode", "unknown mode"},
		{"max_runs zero", "[fleet]\nmax_runs = 0\n", "", "repo", "fleet.max_runs", "at least 1"},
		{"bad lease", "[fleet]\nlease = \"soon\"\n", "", "repo", "fleet.lease", "not a duration"},
		{"short lease", "[fleet]\nlease = \"10s\"\n", "", "repo", "fleet.lease", "too short"},
		{"bad approver", "[team]\napprovers = [\"a b\"]\n", "", "repo", "team.approvers", "not a forge login"},
		{"negative capacity", "", "fleet_capacity = -1\n", "machine", "fleet_capacity", "0 or more"},
		{"neither way", "", "[machines.mini]\n", "machine", "machines.mini", "ssh"},
		{"bad destination", "", "[machines.mini]\nssh = \"-oProxyCommand=x\"\n", "machine", "machines.mini.ssh", "not an SSH destination"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Tables go after the workflow's tables would swallow them, so
			// top-level keys first and tables last.
			repo := minimal + "\n" + c.repo
			if !strings.HasPrefix(c.repo, "[") {
				repo = c.repo + minimal
			}
			f := newFixture(t, repo, c.machine)
			file := f.repoPath()
			if c.file == "machine" {
				file = f.machine
			}
			wantProblem(t, f.problems(), file, c.key, c.frag)
		})
	}
}

// Scenario: Both ways given.
func TestMachineBothWaysGiven(t *testing.T) {
	f := newFixture(t, minimal, "[machines.mini]\nssh = \"mini\"\nhc = \"mini\"\n")
	ps := f.problems()
	wantProblem(t, ps, f.machine, "machines.mini", "not both")
}

// Scenario: Fleet without a remote.
func TestFleetWithoutRemote(t *testing.T) {
	f := newFixture(t, "mode = \"fleet\"\n"+minimal, "")
	_, err := Load(Options{RepoRoot: f.root, MachinePath: f.machine, PushURL: func(remote string) (string, error) {
		return "", errors.New("no such remote '" + remote + "'")
	}})
	var ps Problems
	if !errors.As(err, &ps) {
		t.Fatalf("Load: %v", err)
	}
	wantProblem(t, ps, f.repoPath(), "mode", "fleet mode needs a remote", "origin")

	// With a remote, fleet mode loads.
	f.remote = "git@github.com:sam/app.git"
	if _, err := f.load(); err != nil {
		t.Fatalf("fleet with a remote: %v", err)
	}
}

// Scenario: Team with local shipping.
func TestTeamWithLocalShipping(t *testing.T) {
	f := newFixture(t, "mode = \"team\"\nship = \"local\"\n"+minimal+"\n[team]\napprovers = [\"sam\"]\n", "")
	wantProblem(t, f.problems(), f.repoPath(), "mode", `team mode needs ship = "mr" or "merge"`)
}

func TestTeamNeedsApprovers(t *testing.T) {
	f := newFixture(t, "mode = \"team\"\nship = \"mr\"\n"+minimal, "")
	wantProblem(t, f.problems(), f.repoPath(), "team.approvers", "needs approvers")

	// Without the approval gate, no approvers are needed.
	f = newFixture(t, "mode = \"team\"\nship = \"mr\"\nrequire_approval = false\n"+minimal, "")
	for _, p := range f.problems() {
		if p.Key == "team.approvers" || p.Key == "mode" {
			t.Fatalf("unexpected problem %v", p)
		}
	}
}

func TestLoadMachine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	writeFile(t, path, "machine = \"studio\"\nfleet_capacity = 2\n\n[machines.mini]\nssh = \"mini\"\n\n[agents.omp]\ncommand = [\"omp\", \"{brief}\"]\n")
	cfg, err := LoadMachine(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Machine != "studio" || cfg.FleetCapacity != 2 || cfg.Machines["mini"].SSH != "mini" || cfg.Agents["omp"].Command[0] != "omp" {
		t.Fatalf("%+v", cfg)
	}
	// A missing machine config is empty.
	cfg, err = LoadMachine(filepath.Join(dir, "none.toml"))
	if err != nil || cfg.FleetCapacity != 0 || len(cfg.Machines) != 0 {
		t.Fatalf("missing file: %+v, %v", cfg, err)
	}
	// Problems of the machine file are reported.
	writeFile(t, path, "[machines.mini]\nssh = \"a\"\nhc = \"b\"\n")
	_, err = LoadMachine(path)
	var ps Problems
	if !errors.As(err, &ps) {
		t.Fatalf("want problems, got %v", err)
	}
	wantProblem(t, ps, path, "machines.mini", "not both")
}
