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
