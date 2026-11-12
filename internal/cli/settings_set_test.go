package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

const setSidecar = `# Owner's notes
depends_on = ["fix-login"] # after the fix

[approval]
hash = "sha256:0000"
by = "Someone"
at = 2026-11-01T09:00:00+10:00
`

// change-settings, scenario Set an agent: codex is allowed; set writes
// [steps.build] agent = "codex" in the checkout (the change has no
// worktree), keeps the comments, depends_on and [approval] lines as they
// were, commits nothing and says so. --json gives the same result for the
// board.
func TestSetAnAgent(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"},
		Extra: "\n[overrides]\nagents = [\"codex\"]\nmodels = { codex = [\"gpt-5.5\"] }\n"})
	f.setSidecar("add-login", setSidecar)
	head := testenv.Git(t, f.repo, "rev-parse", "HEAD")
	path := filepath.Join(f.changeDir("add-login"), "tuckerbox.toml")

	res := f.tb("set", "add-login", "step.build.agent=codex")
	if res.code != 0 {
		t.Fatalf("set: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	want := setSidecar + "\n[steps.build]\nagent = \"codex\"\n"
	if got := readFile(t, path); got != want {
		t.Errorf("sidecar\n%s\nwant\n%s", got, want)
	}
	for _, s := range []string{"set step.build.agent=codex for add-login", "repository checkout", "not committed", "committed and pushed"} {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("output lacks %q:\n%s", s, res.stdout)
		}
	}
	if got := testenv.Git(t, f.repo, "rev-parse", "HEAD"); got != head {
		t.Error("set made a commit")
	}
	if out := testenv.Git(t, f.repo, "status", "--porcelain"); !strings.Contains(out, "openspec/changes/add-login/tuckerbox.toml") {
		t.Errorf("sidecar not left uncommitted: %q", out)
	}
	if res := f.tb("check"); res.code != 0 {
		t.Errorf("check after set: exit %d: %s", res.code, res.stderr)
	}
	sc, ps, err := settings.Read(f.repo, "add-login")
	if err != nil || len(ps) != 0 || sc.Steps["build"].Agent != "codex" || len(sc.DependsOn) != 1 {
		t.Errorf("read back: %+v %v %v", sc, ps, err)
	}

	// Again: nothing changes.
	if res := f.tb("set", "add-login", "step.build.agent=codex"); res.code != 0 || !strings.Contains(res.stdout, "already codex; nothing changed") {
		t.Errorf("same value: %d %s %s", res.code, res.stdout, res.stderr)
	}

	// --json, with a model for the new agent, in the same table.
	res = f.tb("set", "--json", "add-login", "step.build.model=gpt-5.5")
	var doc setResult
	if res.code != 0 || json.Unmarshal([]byte(res.stdout), &doc) != nil {
		t.Fatalf("json: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if doc.Version != 1 || doc.Change != "add-login" || doc.Key != "step.build.model" || doc.Value != "gpt-5.5" ||
		!doc.Changed || doc.Unset || doc.Committed || doc.Worktree || doc.File != "openspec/changes/add-login/tuckerbox.toml" ||
		!strings.Contains(doc.Note, "not committed") || !sameFile(doc.Dir, f.repo) {
		t.Errorf("json result %+v", doc)
	}
	if got := readFile(t, path); got != want+"model = \"gpt-5.5\"\n" {
		t.Errorf("after model:\n%s", got)
	}

	// machines, comma-separated, before the first table.
	if res := f.tb("set", "add-login", "machines=studio,mini"); res.code != 0 {
		t.Fatalf("machines: %d %s", res.code, res.stderr)
	}
	if got := readFile(t, path); !strings.Contains(got, "# after the fix\nmachines = [\"studio\", \"mini\"]\n\n[approval]") {
		t.Errorf("machines:\n%s", got)
	}

	// Usage errors.
	for _, args := range [][]string{
		{"set", "add-login"},
		{"set", "add-login", "step.build.agent"},
		{"set", "add-login", "--unset", "machines", "machines=a"},
		{"set", "add-login", "--bogus"},
	} {
		if res := f.tb(args...); res.code != 2 {
			t.Errorf("%v: exit %d %s", args, res.code, res.stderr)
		}
	}
	if res := f.tb("set", "nope", "step.build.agent=codex"); res.code == 0 || !strings.Contains(res.stderr, "no change nope") {
		t.Errorf("unknown change: %d %s", res.code, res.stderr)
	}
}

// change-settings, scenario Set a value not allowed: max is not in
// [overrides] efforts, so set exits non-zero, lists the allowed efforts
// and leaves the sidecar as it was. Unknown steps, forbidden keys and a
// repository with no allow-list are refused the same way.
func TestSetValueNotAllowed(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"},
		Extra: "\n[overrides]\nefforts = [\"low\", \"high\"]\n"})
	f.setSidecar("add-login", setSidecar)
	path := filepath.Join(f.changeDir("add-login"), "tuckerbox.toml")

	res := f.tb("set", "add-login", "step.build.effort=max")
	f.refused(res, `steps.build.effort: effort "max" is not allowed; allowed efforts: low, high`)
	if !strings.Contains(res.stderr, "change add-login") || !strings.Contains(res.stderr, "nothing changed") {
		t.Errorf("stderr: %s", res.stderr)
	}
	if got := readFile(t, path); got != setSidecar {
		t.Errorf("sidecar changed:\n%s", got)
	}

	res = f.tb("set", "--json", "add-login", "step.build.effort=max")
	var doc setResult
	if res.code == 0 || json.Unmarshal([]byte(res.stdout), &doc) != nil || !strings.Contains(doc.Error, "allowed efforts: low, high") || doc.Changed {
		t.Errorf("json refusal: %d %s", res.code, res.stdout)
	}

	f.refused(f.tb("set", "add-login", "step.biuld.effort=high"), `unknown step "biuld"`)
	f.refused(f.tb("set", "add-login", "step.build.agent=codex"), "the repository allows no change to override agent")
	f.refused(f.tb("set", "add-login", "step.build.args=--dangerously-skip-permissions"), "unknown key")
	f.refused(f.tb("set", "add-login", "step.build.effort="), "empty value; use --unset")
	if got := readFile(t, path); got != setSidecar {
		t.Errorf("sidecar changed:\n%s", got)
	}
	if res := f.tb("set", "add-login", "step.build.effort=high"); res.code != 0 {
		t.Errorf("allowed effort: %d %s", res.code, res.stderr)
	}

	// No allow-list at all.
	g := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	g.refused(g.tb("set", "add-login", "step.review.effort=low"), "the repository allows no change overrides")
	if _, err := os.Stat(filepath.Join(g.changeDir("add-login"), "tuckerbox.toml")); err == nil {
		t.Error("sidecar written without an allow-list")
	}
}

// change-settings, scenario Back to the repository default: removing the
// build step's only override removes its table, and the step uses the
// repository's model again. Removing it again changes nothing and exits 0.
func TestSetUnsetToRepositoryDefault(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login", "fix-login"},
		Extra: "\n[overrides]\nmodels = { fake = [\"sonnet\"] }\n"})
	f.setRepoConfig("role = \"builder\"\n", "role = \"builder\"\nmodel = \"opus\"\n")
	f.setSidecar("add-login", setSidecar+"\n[steps.build]\nmodel = \"sonnet\"\n")
	path := filepath.Join(f.changeDir("add-login"), "tuckerbox.toml")

	res := f.tb("set", "add-login", "--unset", "step.build.model")
	if res.code != 0 || !strings.Contains(res.stdout, "removed step.build.model") || !strings.Contains(res.stdout, "not committed") {
		t.Fatalf("unset: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if got := readFile(t, path); got != setSidecar {
		t.Errorf("sidecar\n%s\nwant\n%s", got, setSidecar)
	}
	cfg, err := App{}.placeConfig(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	sc, _, _ := settings.Read(f.repo, "add-login")
	step, _ := cfg.Step("build")
	if e := settings.Resolve(cfg, step, sc.Steps["build"]); e.Model != "opus" || e.From.Model != "repo" {
		t.Errorf("effective model %q from %s", e.Model, e.From.Model)
	}

	res = f.tb("set", "add-login", "--unset", "step.build.model")
	if res.code != 0 || !strings.Contains(res.stdout, "not set") {
		t.Errorf("unset again: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if got := readFile(t, path); got != setSidecar {
		t.Errorf("sidecar changed by a no-op unset")
	}

	// A sidecar left empty is removed.
	f.setSidecar("fix-login", "[steps.build]\nmodel = \"sonnet\"\n")
	if res := f.tb("set", "fix-login", "--unset=step.build.model"); res.code != 0 {
		t.Fatalf("unset fix-login: %d %s", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.changeDir("fix-login"), "tuckerbox.toml")); !os.IsNotExist(err) {
		t.Errorf("empty sidecar kept: %v", err)
	}
}

// change-settings, scenario Set during a run: add-login is running its
// build step in a real run process, which holds the change lock. set
// refuses, says the change is running, and leaves the worktree's sidecar
// as it was. Once the run finishes it can set.
func TestSetDuringRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"},
		Extra: "\n[overrides]\nmodels = { fake = [\"opus\"] }\n"})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "build")
	path := filepath.Join(f.worktree("add-login"), "openspec", "changes", "add-login", "tuckerbox.toml")
	_, statErr := os.Stat(path)

	res := f.tb("set", "add-login", "step.review.model=opus")
	f.refused(res, "running step build")
	if !strings.Contains(res.stderr, "active run") {
		t.Errorf("stderr: %s", res.stderr)
	}
	if _, err := os.Stat(path); (err == nil) != (statErr == nil) {
		t.Error("sidecar written during the run")
	}
	f.refused(f.tb("set", "add-login", "--unset", "step.review.model"), "running step build")

	f.open("build")
	f.waitState("add-login", "ready", "")
	if res := f.tb("set", "add-login", "step.review.model=opus"); res.code != 0 || !strings.Contains(res.stdout, "change's worktree") {
		t.Errorf("set after the run: %d %s %s", res.code, res.stdout, res.stderr)
	}
}

// change-settings, scenario Set while blocked: a run blocked in review
// holds no lock, so set writes the worktree's sidecar; the resumed review
// step runs with effort high, recorded as from the change.
func TestSetWhileBlocked(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"},
		Extra: "\n[overrides]\nefforts = [\"high\"]\n"})
	f.env.SetPlan(t, map[string]testenv.Action{"review": {Status: "rejected", Summary: "not yet"}})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "blocked", "")
	evs, _ := f.historyOf("add-login")
	if r := stepStarted(t, evs, "review"); r.Settings == nil || r.Settings.Effort.Value != "" {
		t.Fatalf("review before the set: %+v", r.Settings)
	}

	res := f.tb("set", "add-login", "step.review.effort=high")
	if res.code != 0 || !strings.Contains(res.stdout, "change's worktree") {
		t.Fatalf("set while blocked: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if got := readFile(t, filepath.Join(f.worktreeOf("add-login"), "openspec", "changes", "add-login", "tuckerbox.toml")); got != "[steps.review]\neffort = \"high\"\n" {
		t.Errorf("worktree sidecar %q", got)
	}

	f.env.SetPlan(t, map[string]testenv.Action{})
	if res := f.tb("resume", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("resume: %d %s %s", res.code, res.stdout, res.stderr)
	}
	f.waitState("add-login", "ready", "")
	evs, _ = f.historyOf("add-login")
	var last *history.Event
	for i := range evs {
		if evs[i].Kind == history.StepStarted && evs[i].Step == "review" {
			last = &evs[i]
		}
	}
	want := history.Setting{Value: "high", Source: "change"}
	if last == nil || last.Settings == nil || last.Settings.Effort != want {
		t.Errorf("resumed review settings %+v, want effort %+v", last, want)
	}
}
