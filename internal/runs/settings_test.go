package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// commitSidecar commits change id's tuckerbox.toml in the fixture's checkout.
func (f *fixture) commitSidecar(id, data string) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.dir, "openspec", "changes", id, "tuckerbox.toml"), data)
	testenv.Git(f.t, f.dir, "add", "-A")
	testenv.Git(f.t, f.dir, "commit", "-q", "-m", "settings of "+id)
}

// change-settings, Runs refuse invalid overrides, under the change lock:
// with the allow-list narrowed after a run blocked, the engine refuses to
// resume or rerun it before run.json changes, and refuses to start a new
// change before making its worktree (judging the commit it would use).
func TestEngineRefusesOverridesNoLongerAllowed(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"}})
	f.cfg.Overrides = &config.Overrides{Efforts: []string{"high"}}
	f.commitSidecar("add-login", "[steps.build]\neffort = \"high\"\n")
	f.commitSidecar("fix-login", "[steps.build]\neffort = \"high\"\n")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "a question"}})
	if r := f.run(Request{Change: "fix-login", Mode: Start}); r.State != Blocked {
		t.Fatalf("fix-login is %s, want blocked", r.State)
	}
	before, err := ReadRun(f.state("fix-login").RunFile())
	if err != nil {
		t.Fatal(err)
	}

	f.cfg.Overrides.Efforts = []string{"medium"}
	ctx := context.Background()
	for _, req := range []Request{
		{Change: "fix-login", Mode: Resume, Worktree: f.worktree("fix-login")},
		{Change: "fix-login", Mode: Rerun, Step: "build", Worktree: f.worktree("fix-login")},
		{Change: "fix-login", Mode: Start},
		{Change: "add-login", Mode: Start},
	} {
		_, err := f.engine().Run(ctx, req)
		var oe *OverridesError
		if !errors.As(err, &oe) || !strings.Contains(err.Error(), `steps.build.effort: effort "high" is not allowed; allowed efforts: medium`) {
			t.Errorf("%s %v: err %v, want an overrides refusal", req.Change, req.Mode, err)
		}
	}
	after, err := ReadRun(f.state("fix-login").RunFile())
	if err != nil {
		t.Fatal(err)
	}
	if after.State != Blocked || after.Round != before.Round || !after.Since.Equal(before.Since) {
		t.Errorf("run.json changed: %+v -> %+v", before, after)
	}
	if _, err := os.Stat(f.worktree("add-login")); err == nil {
		t.Error("a worktree was made for add-login")
	}
}

// change-settings, scenario Profile not granted here, at step start: the
// change's settings are read as each step starts, so a profile picked in
// the worktree mid-run that this machine does not define stops the run
// before that step's agent starts, naming the profile.
func TestStepStartRefusesProfileNotGranted(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.cfg.Overrides = &config.Overrides{Permissions: []string{"edit"}}
	gate := filepath.Join(f.env.Home, "gate-build")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: gate}})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch := f.start(ctx, Request{Change: "add-login", Mode: Start})
	for len(f.env.Starts(t)) == 0 {
		if ctx.Err() != nil {
			t.Fatal("build never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	testenv.WriteFile(t, filepath.Join(f.worktree("add-login"), "openspec", "changes", "add-login", "tuckerbox.toml"), "[steps.review]\npermissions = \"edit\"\n")
	testenv.WriteFile(t, gate, "go\n")
	res := <-ch
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.run.State != Blocked || res.run.Step != "review" || !strings.Contains(res.run.Reason, `permission profile "edit" is not defined in machine config`) {
		t.Errorf("run: %s at %s: %s", res.run.State, res.run.Step, res.run.Reason)
	}
	evs, err := history.Read(history.File(f.state("add-login")))
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Kind == history.StepStarted && ev.Step == "review" {
			t.Errorf("review started: %+v", ev)
		}
	}
	if starts := f.env.Starts(t); len(starts) != 1 {
		t.Errorf("agent starts %v, want only build", starts)
	}
}
