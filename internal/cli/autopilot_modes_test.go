package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/dispatch"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// Scenario: Fleet mode: a pass runs tuckerbox dispatch, which places the
// approved change on mini; nothing starts on this machine itself, and a
// run mini reports blocked is reported as needing a person.
func TestAutopilotFleetMode(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "mini")
	f.approveAll("add-login", "add-users")
	f.report("mini", dispatch.Report{FleetCapacity: 2, FleetRuns: 1, Checkout: true,
		Runs: []dispatch.RunReport{{Change: "add-users", State: "blocked", Dispatched: true, Mode: "fleet"}}})
	f.claim("add-users", "mini", dispatch.ClaimParked, time.Now())
	f.answer("mini", "add-login: started (pid 7, queued) in /w\n", 0)

	dry := f.pass("--dry-run")
	if !strings.Contains(dry.stdout, "would dispatch: start add-login on mini") {
		t.Errorf("dry run:\n%s", dry.stdout)
	}
	if c := f.startCalls("mini"); len(c) != 0 {
		t.Fatalf("dry run started %v", c)
	}

	res := f.pass()
	for _, want := range []string{"dispatch: start add-login on mini", "add-users needs you: blocked on mini"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s\n%s", want, res.stdout, res.stderr)
		}
	}
	if c := f.startCalls("mini"); len(c) != 1 || !strings.Contains(c[0], "--dispatched add-login") {
		t.Errorf("starts on mini: %v", c)
	}
	f.noRun("add-login")
	f.noRun("add-users")
}

// Scenario: Team mode: autopilot exits non-zero, says it is for solo and
// fleet repositories, and starts nothing.
func TestAutopilotTeamMode(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data, _ := os.ReadFile(p)
	text := strings.Replace(string(data), "ship = \"local\"\n", "ship = \"mr\"\n", 1)
	testenv.WriteFile(t, p, "mode = \"team\"\n"+text+"\n[team]\napprovers = [\"sam\"]\n")
	f.commit("team")
	testenv.Git(t, f.repo, "remote", "add", "origin", "https://github.com/sam/app.git")
	res := f.tb("autopilot", "--once")
	if res.code == 0 || !strings.Contains(res.stderr, "autopilot is for solo and fleet repositories") {
		t.Errorf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.repo, ".git", "tuckerbox", "autopilot.json")); err == nil {
		t.Error("left a marker")
	}
	f.noRun("add-login")
}

// A run keeps the mode it started in: a solo run with group 1 done is
// still given its next group by autopilot after the repository moves to
// fleet (dispatch skips a run it holds no claim for), while a new change
// is left to dispatch.
func TestAutopilotSoloRunAfterSwitchToFleet(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}, RequireApproval: true})
	f.withRemote()
	f.tasks("add-login", threeGroups)
	f.push()
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {Tick: []string{"1.1"}}})
	if res := f.pass(); !strings.Contains(res.stdout, "start add-login --tasks next") {
		t.Fatalf("solo pass:\n%s", res.stdout)
	}
	f.waitState("add-login", "ready", "")
	if r := f.runOf("add-login"); r.Scope == nil || r.Scope.Spec != "1" || r.Mode != "solo" {
		t.Fatalf("group 1: %+v %s", r.Scope, r.Mode)
	}
	if res := f.pass("--dry-run"); !strings.Contains(res.stdout, "would start add-login --tasks next") {
		t.Fatalf("solo dry run:\n%s", res.stdout)
	}

	// Move the repository to fleet, with mini as the only other machine.
	ssh := useFakeSSH(t)
	f.setTop("mode = \"fleet\"\n")
	f.push()
	machineConfig(t, f.env.Home, "machine = \"studio\"\n\n[machines.mini]\nssh = \"mini\"\n")
	fl := &fleetFixture{cliFixture: f, ssh: ssh}
	f.approveAll("add-users")
	fl.report("mini", dispatch.Report{FleetCapacity: 2, Checkout: true})

	res := f.pass("--dry-run")
	for _, want := range []string{"would start add-login --tasks next", "would dispatch: start add-users on mini"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("fleet dry run lacks %q:\n%s\n%s", want, res.stdout, res.stderr)
		}
	}
	if strings.Contains(res.stdout, "would start add-users") || strings.Contains(res.stdout, "dispatch: continue add-login") {
		t.Errorf("wrong placement:\n%s", res.stdout)
	}
	if c := fl.startCalls("mini"); len(c) != 0 {
		t.Errorf("dry run started %v", c)
	}
}
