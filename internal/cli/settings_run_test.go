package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// change-settings, scenario Machines in solo mode: a change that prefers
// machine mini runs on studio when the user runs it there; the preference
// is kept for display and placement, and passes tuckerbox check without
// an allow-list.
func TestMachinesInSoloMode(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	testenv.WriteFile(t, filepath.Join(f.env.Home, "config", "tuckerbox", "config.toml"), "machine = \"studio\"\n")
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "fix-login", "tuckerbox.toml"), "machines = [\"mini\"]\n")
	f.commit("prefer mini")

	if res := f.tb("check"); res.code != 0 {
		t.Fatalf("check: exit %d: %s", res.code, res.stderr)
	}
	sc, ps, err := settings.Read(f.repo, "fix-login")
	if err != nil || len(ps) != 0 || !slices.Equal(sc.Machines, []string{"mini"}) {
		t.Fatalf("sidecar: %+v %v %v", sc, ps, err)
	}
	res := f.tb("run", "--foreground", "fix-login")
	if res.code != 0 {
		t.Fatalf("run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "step build round 1") {
		t.Errorf("build did not start:\n%s", res.stdout)
	}
	if s, _ := f.state("fix-login"); s != "ready" {
		t.Errorf("state %s", s)
	}
}

// machineConfig writes this test's machine config.
func (f *cliFixture) machineConfig(data string) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.env.Home, "config", "tuckerbox", "config.toml"), data)
}

// fakeCodex is machine config that makes the built-in codex agent the fake
// agent, passing {model} so the step log shows what it was given.
func fakeCodex() string {
	return fmt.Sprintf("[agents.codex]\ncommand = [%q, \"--model={model}\", \"{brief}\"]\n", tools.FakeAgent)
}

// setRepoConfig rewrites the repository config with old replaced by new
// and commits it.
func (f *cliFixture) setRepoConfig(old, new string) {
	f.t.Helper()
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data := readFile(f.t, p)
	if !strings.Contains(data, old) {
		f.t.Fatalf("config lacks %q:\n%s", old, data)
	}
	testenv.WriteFile(f.t, p, strings.Replace(data, old, new, 1))
	f.commit("config: " + new)
}

func (f *cliFixture) setSidecar(id, data string) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.changeDir(id), "tuckerbox.toml"), data)
	f.commit("settings of " + id)
}

// historyOf reads id's history.jsonl in its worktree.
func (f *cliFixture) historyOf(id string) ([]history.Event, string) {
	f.t.Helper()
	st, err := statedir.Resolve(context.Background(), f.worktreeOf(id))
	if err != nil {
		f.t.Fatal(err)
	}
	evs, err := history.Read(history.File(st))
	if err != nil {
		f.t.Fatal(err)
	}
	return evs, readFile(f.t, history.File(st))
}

func stepStarted(t *testing.T, evs []history.Event, step string) history.Event {
	t.Helper()
	for _, ev := range evs {
		if ev.Kind == history.StepStarted && ev.Step == step {
			return ev
		}
	}
	t.Fatalf("no step_started for %s in %+v", step, evs)
	return history.Event{}
}

// change-settings, scenario Profile not granted here: the repository
// allows profile edit and the change picks it for its build step, but this
// machine defines no profile edit. The run refuses, naming the profile as
// for a repository step, and makes no worktree.
func TestProfileNotGrantedHere(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[overrides]\npermissions = [\"edit\"]\n"})
	f.setSidecar("add-login", "[steps.build]\npermissions = \"edit\"\n")

	for _, args := range [][]string{{"run", "add-login"}, {"run", "--foreground", "add-login"}} {
		res := f.tb(args...)
		f.refused(res, `steps.build.permissions: permission profile "edit" is not defined in machine config`)
	}
	f.noWorktree("add-login")
	if s, _ := f.state("add-login"); s != "" {
		t.Errorf("run state %q", s)
	}

	// Granted on this machine for this repository: the build step runs
	// with it, recorded by name.
	f.machineConfig("[permissions.edit]\nallowed_remotes = [\"*\"]\nargs = { fake = [\"--fake-edit-flag\"] }\n")
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	evs, raw := f.historyOf("add-login")
	if got := stepStarted(t, evs, "build").Settings; got == nil || got.Permissions != (history.Setting{Value: "edit", Source: "change"}) {
		t.Errorf("build settings: %+v", got)
	}
	if strings.Contains(raw, "--fake-edit-flag") {
		t.Errorf("history records the profile's flags:\n%s", raw)
	}
}

// change-settings, scenario Allow-list narrowed later: changes set agent
// codex while the repository allowed it; then the repository removes it.
// run refuses before making a worktree, and resume and rerun refuse before
// the run state changes, each naming the override.
func TestAllowListNarrowedLater(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"}, Extra: "\n[overrides]\nagents = [\"codex\"]\n"})
	f.machineConfig(fakeCodex())
	f.setSidecar("add-login", "[steps.build]\nagent = \"codex\"\n")
	f.setSidecar("fix-login", "[steps.build]\nagent = \"codex\"\n")
	f.env.SetPlan(t, map[string]testenv.Action{"fix-login:build": {Status: "blocked", Summary: "needs a decision"}})
	if res := f.tb("run", "--foreground", "fix-login"); res.code == 0 && !strings.Contains(res.stdout, "blocked") {
		t.Fatalf("run fix-login: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if s, _ := f.state("fix-login"); s != "blocked" {
		t.Fatalf("fix-login is %q, want blocked", s)
	}
	before := f.runOf("fix-login")

	f.setRepoConfig("agents = [\"codex\"]", "agents = [\"claude\"]")
	want := `steps.build.agent: agent "codex" is not allowed; allowed agents: claude`

	f.refused(f.tb("run", "add-login"), want)
	f.refused(f.tb("run", "--foreground", "add-login"), want)
	f.noWorktree("add-login")

	for _, args := range [][]string{
		{"resume", "fix-login"}, {"resume", "--foreground", "fix-login"},
		{"rerun", "fix-login", "build"}, {"run", "fix-login"},
	} {
		f.refused(f.tb(args...), want)
	}
	after := f.runOf("fix-login")
	if after.State != before.State || after.Round != before.Round || !after.Since.Equal(before.Since) {
		t.Errorf("run state changed: %+v -> %+v", before, after)
	}
	f.env.SetPlan(t, map[string]testenv.Action{})
}

// change-settings, scenario History shows the override; and scenario
// Agent changed without a model, end to end: the repository's build step
// is the fake agent with model opus, the change sets only agent codex. The
// build step runs codex with no model, and its step_started event and
// run.json record agent codex from the change and the model from the
// default. A repository profile is recorded by name, never its flags.
func TestHistoryShowsOverride(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"},
		Extra: "permissions = \"edit\"\n\n[overrides]\nagents = [\"codex\"]\n"})
	f.setRepoConfig("role = \"builder\"\n", "role = \"builder\"\nmodel = \"opus\"\n")
	f.machineConfig(fakeCodex() + "\n[permissions.edit]\nallowed_remotes = [\"*\"]\nargs = { fake = [\"--fake-edit-flag\"] }\n")
	f.setSidecar("add-login", "[steps.build]\nagent = \"codex\"\n")

	if res := f.tb("check"); res.code != 0 {
		t.Fatalf("check: exit %d: %s", res.code, res.stderr)
	}
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	evs, raw := f.historyOf("add-login")
	build := stepStarted(t, evs, "build")
	want := history.Settings{
		Agent:       history.Setting{Value: "codex", Source: "change"},
		Model:       history.Setting{Source: "default"},
		Effort:      history.Setting{Source: "default"},
		Permissions: history.Setting{Source: "default"},
	}
	if build.Agent != "codex" || build.Settings == nil || *build.Settings != want {
		t.Errorf("build step_started: agent %q settings %+v, want %+v", build.Agent, build.Settings, want)
	}
	for _, ev := range evs {
		if ev.Step == "build" && ev.Agent != "codex" {
			t.Errorf("%s event for build has agent %q", ev.Kind, ev.Agent)
		}
	}
	review := stepStarted(t, evs, "review")
	wantReview := history.Settings{
		Agent:       history.Setting{Value: "fake", Source: "repo"},
		Model:       history.Setting{Source: "default"},
		Effort:      history.Setting{Source: "default"},
		Permissions: history.Setting{Value: "edit", Source: "repo"},
	}
	if review.Settings == nil || *review.Settings != wantReview {
		t.Errorf("review settings %+v, want %+v", review.Settings, wantReview)
	}
	if strings.Contains(raw, "--fake-edit-flag") {
		t.Errorf("history records the profile's flags:\n%s", raw)
	}
	if !strings.Contains(raw, `"settings":{"agent":{"value":"codex","source":"change"},"model":{"source":"default"}`) {
		t.Errorf("history line format:\n%s", raw)
	}

	// run.json records the last step's settings.
	r := f.runOf("add-login")
	if r.StepSettings == nil || r.StepSettings.Step != "review" || r.StepSettings.Settings != wantReview {
		t.Errorf("run.json step_settings %+v", r.StepSettings)
	}

	// The codex step was given no model, not the repository's opus.
	logs := f.tb("logs", "add-login", "build")
	if logs.code != 0 || !strings.Contains(logs.stdout, "fakeagent") {
		t.Fatalf("logs: exit %d\n%s\n%s", logs.code, logs.stdout, logs.stderr)
	}
	if strings.Contains(logs.stdout, "opus") || strings.Contains(logs.stdout, "--model=") {
		t.Errorf("build log shows a model:\n%s", logs.stdout)
	}
}
