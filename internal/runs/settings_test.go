package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

// writeRepoConfig rewrites the repository config in dir (the checkout or
// a worktree) with old replaced by new, without committing.
func (f *fixture) writeRepoConfig(dir, old, new string) {
	f.t.Helper()
	p := filepath.Join(dir, ".tuckerbox", "config.toml")
	data, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		f.t.Fatalf("%s lacks %q:\n%s", p, old, data)
	}
	testenv.WriteFile(f.t, p, strings.Replace(string(data), old, new, 1))
}

// change-settings, Runs refuse invalid overrides, under the change lock:
// with the allow-list narrowed after a run blocked, the engine refuses to
// resume or rerun it before run.json changes, and refuses to start a new
// change before making its worktree (judging the commit it would use).
// The allow-list is read from the worktree, else that commit; the engine's
// own config, loaded from the checkout, still allows the override and
// does not count.
func TestEngineRefusesOverridesNoLongerAllowed(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"}, Extra: "\n[overrides]\nefforts = [\"high\"]\n"})
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

	// Narrowed in fix-login's worktree, and in a commit on the branch a
	// new worktree starts from; the checkout's file then goes back to
	// allowing high, uncommitted.
	f.writeRepoConfig(f.worktree("fix-login"), `efforts = ["high"]`, `efforts = ["medium"]`)
	f.writeRepoConfig(f.dir, `efforts = ["high"]`, `efforts = ["medium"]`)
	testenv.Git(t, f.dir, "commit", "-q", "-am", "narrow efforts")
	f.writeRepoConfig(f.dir, `efforts = ["medium"]`, `efforts = ["high"]`)
	if f.cfg.Overrides == nil || !slices.Equal(f.cfg.Overrides.Efforts, []string{"high"}) {
		t.Fatalf("engine config overrides %+v, want efforts high", f.cfg.Overrides)
	}

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

// The other way round: the checkout's config allows nothing, but the
// worktree's allows the override, so the run uses it. What runs is judged
// by the files the run starts from.
func TestEngineUsesWorktreeAllowList(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[overrides]\nefforts = [\"high\"]\n"})
	f.commitSidecar("add-login", "[steps.build]\neffort = \"high\"\n")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Status: "blocked", Summary: "a question"}})
	if r := f.run(Request{Change: "add-login", Mode: Start}); r.State != Blocked {
		t.Fatalf("add-login is %s, want blocked", r.State)
	}
	f.cfg.Overrides = nil
	f.env.SetPlan(t, map[string]testenv.Action{})
	r := f.run(Request{Change: "add-login", Mode: Rerun, Step: "build", Worktree: f.worktree("add-login")})
	if r.State == Blocked && strings.Contains(r.Reason, "not allowed") {
		t.Fatalf("rerun refused: %s", r.Reason)
	}
	evs, err := history.Read(history.File(f.state("add-login")))
	if err != nil {
		t.Fatal(err)
	}
	var last *history.Settings
	for _, ev := range evs {
		if ev.Kind == history.StepStarted && ev.Step == "build" {
			last = ev.Settings
		}
	}
	if last == nil || last.Effort != (history.Setting{Value: "high", Source: "change"}) {
		t.Errorf("build settings %+v, want effort high from the change", last)
	}
}

// Execution reads the allow-list where the step runs: the worktree's
// config, narrowed mid-run, stops the next step that uses the override,
// although the engine's config still allows it.
func TestStepStartUsesWorktreeAllowList(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[overrides]\nefforts = [\"high\"]\n"})
	f.commitSidecar("add-login", "[steps.review]\neffort = \"high\"\n")
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
	f.writeRepoConfig(f.worktree("add-login"), `efforts = ["high"]`, `efforts = ["medium"]`)
	testenv.WriteFile(t, gate, "go\n")
	res := <-ch
	if res.err != nil {
		t.Fatal(res.err)
	}
	if res.run.State != Blocked || res.run.Step != "review" || !strings.Contains(res.run.Reason, `steps.review.effort: effort "high" is not allowed; allowed efforts: medium`) {
		t.Errorf("run: %s at %s: %s", res.run.State, res.run.Step, res.run.Reason)
	}
	if starts := f.env.Starts(t); len(starts) != 1 {
		t.Errorf("agent starts %v, want only build", starts)
	}
}

// change-settings, scenario Profile not granted here, at step start: the
// change's settings are read as each step starts, so a profile picked in
// the worktree mid-run that this machine does not define stops the run
// before that step's agent starts, naming the profile.
func TestStepStartRefusesProfileNotGranted(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[overrides]\npermissions = [\"edit\"]\n"})
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

// change-settings, Resume checks approved settings: the engine's
// SettingsGate judges every Resume and Rerun under the change lock, before
// the run state changes, and never a Start.
func TestSettingsGateOnResumeAndRerun(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {Status: "blocked"}})
	var asked []string
	refuse := func(_ context.Context, change, worktree string) error {
		asked = append(asked, change+" "+filepath.Base(worktree))
		return errors.New("step.review.agent changed since approval")
	}
	e := f.engine()
	e.SettingsGate = refuse
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Blocked || len(asked) != 0 {
		t.Fatalf("start: %+v %v, gate asked %q", r, err, asked)
	}
	before := *f.current("add-login")
	for _, req := range []Request{
		{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")},
		{Change: "add-login", Mode: Rerun, Step: "build", Worktree: f.worktree("add-login")},
	} {
		e := f.engine()
		e.SettingsGate = refuse
		if _, err := e.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "step.review.agent") {
			t.Errorf("mode %v: %v", req.Mode, err)
		}
		if now := f.current("add-login"); now.State != Blocked || !now.Since.Equal(before.Since) || now.Round != before.Round {
			t.Errorf("mode %v changed the run: %+v", req.Mode, now)
		}
	}
	if len(asked) != 2 {
		t.Errorf("gate asked %q", asked)
	}
	f.env.SetPlan(t, map[string]testenv.Action{})
	e = f.engine()
	e.SettingsGate = func(context.Context, string, string) error { return nil }
	if r, err := e.Run(context.Background(), Request{Change: "add-login", Mode: Resume, Worktree: f.worktree("add-login")}); err != nil || r.State != Ready {
		t.Errorf("allowed resume: %+v %v", r, err)
	}
}
