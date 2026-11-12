package settings

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

func TestParseKeepsOtherKeys(t *testing.T) {
	data := `title = "Login page"
depends_on = ["add-users", " add-users "]
priority = 3000
machines = ["studio", "mini"]

[steps.build]
agent = "codex"
model = "gpt-5.5"
effort = "high"

[steps.review]
permissions = "readonly"

[approval]
hash = "sha256:00"
by = "Sam Example"
at = 2026-11-12T09:00:00+10:00
`
	sc, ps, err := Parse([]byte(data))
	if err != nil || len(ps) != 0 {
		t.Fatalf("Parse: %v %v", err, ps)
	}
	if sc.Title != "Login page" || !slices.Equal(sc.DependsOn, []string{"add-users"}) {
		t.Errorf("title or depends_on: %+v", sc)
	}
	if !slices.Equal(sc.Machines, []string{"studio", "mini"}) {
		t.Errorf("machines: %v", sc.Machines)
	}
	if got := sc.Steps["build"]; got != (Override{Agent: "codex", Model: "gpt-5.5", Effort: "high"}) {
		t.Errorf("build: %+v", got)
	}
	if got := sc.Steps["review"]; got != (Override{Permissions: "readonly"}) {
		t.Errorf("review: %+v", got)
	}
}

func TestParseEmptyFile(t *testing.T) {
	sc, ps, err := Parse(nil)
	if err != nil || len(ps) != 0 || len(sc.Steps) != 0 || sc.Machines != nil {
		t.Errorf("Parse(nil): %+v %v %v", sc, ps, err)
	}
}

// change-settings, scenario Raw flags in an override: refused by key,
// saying flags belong in machine config, without the value.
func TestParseRefusesOtherKeys(t *testing.T) {
	data := `[steps.build]
agent = "codex"
args = ["--dangerously-skip-permissions"]
command = ["sh", "-c", "secret-command"]
role = "planner"

[steps.build.sandbox]
mode = "danger-full-access"
`
	sc, ps, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Steps["build"].Agent != "codex" {
		t.Errorf("agent lost: %+v", sc.Steps["build"])
	}
	want := map[string]string{
		"steps.build.args":    "belong in a permission profile in machine config",
		"steps.build.sandbox": "belong in a permission profile in machine config",
		"steps.build.command": "may set only agent, model, effort, permissions",
		"steps.build.role":    "may set only agent, model, effort, permissions",
	}
	if len(ps) != len(want) {
		t.Errorf("problems: %v", ps)
	}
	for _, p := range ps {
		if frag, ok := want[p.Key]; !ok || !strings.Contains(p.Message, frag) {
			t.Errorf("unexpected problem %v", p)
		}
		for _, secret := range []string{"--dangerously-skip-permissions", "secret-command", "danger-full-access"} {
			if strings.Contains(p.String(), secret) {
				t.Errorf("problem prints the value %q: %v", secret, p)
			}
		}
	}
}

// Dotted keys make implicit tables the decoder does not list as undecoded,
// so each refused key in a step table is reported by its own name, once,
// however deep the value sits.
func TestParseRefusesDottedKeys(t *testing.T) {
	data := `[steps.build]
agent = "codex"
args.codex = ["--dangerously-skip-permissions"]
args.claude = ["--dangerously-skip-permissions"]
extra.deep.key = "secret-command"
steps_flags.x = "danger-full-access"

[steps.review.sandbox.inner]
mode = "danger-full-access"
`
	sc, ps, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Steps["build"].Agent != "codex" {
		t.Errorf("agent lost: %+v", sc.Steps["build"])
	}
	want := map[string]string{
		"steps.build.args":        "belong in a permission profile in machine config",
		"steps.build.extra":       "may set only agent, model, effort, permissions",
		"steps.build.steps_flags": "may set only agent, model, effort, permissions",
		"steps.review.sandbox":    "belong in a permission profile in machine config",
	}
	got := map[string]int{}
	for _, p := range ps {
		got[p.Key]++
		if frag, ok := want[p.Key]; !ok || !strings.Contains(p.Message, frag) {
			t.Errorf("unexpected problem %v", p)
		}
		for _, secret := range []string{"--dangerously-skip-permissions", "secret-command", "danger-full-access"} {
			if strings.Contains(p.String(), secret) {
				t.Errorf("problem prints the value %q: %v", secret, p)
			}
		}
	}
	for k := range want {
		if got[k] != 1 {
			t.Errorf("%s reported %d times, want once: %v", k, got[k], ps)
		}
	}
}

func TestParseEmptyValues(t *testing.T) {
	sc, ps, err := Parse([]byte("machines = [\"\"]\n[steps.build]\nmodel = \" \"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Key != "machines" || ps[1].Key != "steps.build.model" {
		t.Errorf("problems: %v", ps)
	}
	if sc.Steps["build"].Model != "" || len(sc.Machines) != 0 {
		t.Errorf("empty values kept: %+v", sc)
	}
}

func TestParseWrongType(t *testing.T) {
	for _, data := range []string{"[steps.build]\nagent = 3\n", "machines = \"mini\"\n", "steps = 1\n"} {
		if _, _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%q: no error", data)
		}
	}
}

// loadCfg loads repository config repo and machine config machine ("" for
// none) for Resolve tests.
func loadCfg(t *testing.T, repo, machine string) *config.Config {
	t.Helper()
	root := t.TempDir()
	write := func(p, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, config.RepoFile), repo)
	opts := config.Options{RepoRoot: root, Remote: "https://example.com/team/app.git"}
	if machine != "" {
		opts.MachinePath = filepath.Join(root, "machine.toml")
		write(opts.MachinePath, machine)
	}
	cfg, err := config.Load(opts)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

const resolveRepo = `[workflow]
steps = ["build", "review"]

[steps.build]
agent = "claude"
role = "builder"
model = "opus"
effort = "high"

[steps.review]
agent = "claude"
role = "reviewer"
effort = "high"

[overrides]
agents = ["claude", "codex"]
efforts = ["medium", "high"]
models = { codex = ["gpt-5.5"] }
`

func resolveStep(t *testing.T, cfg *config.Config, id string, o Override) Effective {
	t.Helper()
	st, ok := cfg.Step(id)
	if !ok {
		t.Fatalf("no step %s", id)
	}
	return Resolve(cfg, st, o)
}

// change-settings, scenario Change wins: the repository sets review effort
// high, the change medium; the step runs with medium, from the change.
func TestResolveChangeWins(t *testing.T) {
	cfg := loadCfg(t, resolveRepo, "")
	sc, ps, err := Parse([]byte("[steps.review]\neffort = \"medium\"\n"))
	if err != nil || len(ps) > 0 || len(Check(cfg, sc)) > 0 {
		t.Fatalf("sidecar: %v %v %v", err, ps, Check(cfg, sc))
	}
	got := resolveStep(t, cfg, "review", sc.Steps["review"])
	want := Effective{Agent: "claude", Effort: "medium", From: config.StepSources{
		Agent: config.SourceRepo, Model: config.SourceDefault, Effort: config.SourceChange, Permissions: config.SourceDefault,
	}}
	if got != want {
		t.Errorf("review: got %+v, want %+v", got, want)
	}
	// With no override, the repository's value, from the repository.
	if got := resolveStep(t, cfg, "review", Override{}); got.Effort != "high" || got.From.Effort != config.SourceRepo {
		t.Errorf("no override: %+v", got)
	}
}

// change-settings, scenario Agent changed without a model: the build step
// is claude with model opus in the repository; a change setting only agent
// codex gets Codex's default model (and effort), not opus.
func TestResolveAgentChangedWithoutModel(t *testing.T) {
	cfg := loadCfg(t, resolveRepo, "")
	got := resolveStep(t, cfg, "build", Override{Agent: "codex"})
	want := Effective{Agent: "codex", From: config.StepSources{
		Agent: config.SourceChange, Model: config.SourceDefault, Effort: config.SourceDefault, Permissions: config.SourceDefault,
	}}
	if got != want {
		t.Errorf("build: got %+v, want %+v", got, want)
	}
	// A default from [agents.codex] is the new agent's default.
	cfg = loadCfg(t, resolveRepo, "[agents.codex]\ncommand = [\"codex\", \"exec\", \"{brief}\"]\nmodel = \"gpt-5.5\"\n")
	if got := resolveStep(t, cfg, "build", Override{Agent: "codex"}); got.Model != "gpt-5.5" || got.From.Model != config.SourceDefault {
		t.Errorf("with an agent default: %+v", got)
	}
	// A model the change sets for the new agent is used, from the change.
	if got := resolveStep(t, cfg, "build", Override{Agent: "codex", Model: "gpt-5.5"}); got.Model != "gpt-5.5" || got.From.Model != config.SourceChange {
		t.Errorf("with a change model: %+v", got)
	}
	// Naming the same agent changes nothing: opus stays.
	if got := resolveStep(t, cfg, "build", Override{Agent: "claude"}); got.Model != "opus" || got.From.Model != config.SourceRepo || got.From.Agent != config.SourceChange {
		t.Errorf("same agent: %+v", got)
	}
}

// Each key falls back change, repository, machine, then default; a
// machine step's value is recorded as from the machine, and a profile
// keeps its name and source.
func TestResolveSources(t *testing.T) {
	repo := strings.Replace(resolveRepo, "[steps.review]\nagent = \"claude\"\nrole = \"reviewer\"\neffort = \"high\"\n", "[steps.review]\nrole = \"reviewer\"\n", 1)
	machine := `[steps.review]
agent = "claude"
model = "sonnet"
permissions = "readonly"

[permissions.readonly]
allowed_remotes = ["*"]
args = { claude = ["--permission-mode", "plan"], codex = ["--sandbox", "read-only"] }
`
	cfg := loadCfg(t, repo, machine)
	got := resolveStep(t, cfg, "review", Override{})
	want := Effective{Agent: "claude", Model: "sonnet", Permissions: "readonly", From: config.StepSources{
		Agent: config.SourceMachine, Model: config.SourceMachine, Effort: config.SourceDefault, Permissions: config.SourceMachine,
	}}
	if got != want {
		t.Errorf("review: got %+v, want %+v", got, want)
	}
	// Another agent drops the machine's model, keeps the profile by name.
	got = resolveStep(t, cfg, "review", Override{Agent: "codex"})
	if got.Model != "" || got.From.Model != config.SourceDefault || got.Permissions != "readonly" || got.From.Permissions != config.SourceMachine {
		t.Errorf("codex: %+v", got)
	}
	run := got.Apply(config.Step{ID: "review", Role: "reviewer", Agent: "claude", Model: "sonnet"})
	if run.Agent != "codex" || run.Model != "" || run.Role != "reviewer" || run.Permissions != "readonly" {
		t.Errorf("Apply: %+v", run)
	}
}

// A change that moves a step to another agent keeps the step's profile; a
// profile that grants that agent nothing is refused, as for a repository
// step.
func TestCheckInheritedProfileForNewAgent(t *testing.T) {
	repo := strings.Replace(resolveRepo, "effort = \"high\"\n\n[steps.review]", "effort = \"high\"\npermissions = \"edit\"\n\n[steps.review]", 1)
	machine := "[permissions.edit]\nallowed_remotes = [\"*\"]\nargs = { claude = [\"--permission-mode\", \"acceptEdits\"] }\n"
	cfg := loadCfg(t, repo, machine)
	ps := Check(cfg, Sidecar{Steps: map[string]Override{"build": {Agent: "codex"}}})
	if len(ps) != 1 || ps[0].Key != "steps.build.agent" || !strings.Contains(ps[0].Message, `permission profile "edit" grants no flags for agent "codex"`) {
		t.Errorf("problems: %v", ps)
	}
	// An allowed agent that is not defined anywhere is refused.
	cfg = loadCfg(t, strings.Replace(resolveRepo, `agents = ["claude", "codex"]`, `agents = ["claude", "gemini"]`, 1), "")
	ps = Check(cfg, Sidecar{Steps: map[string]Override{"build": {Agent: "gemini"}}})
	if len(ps) != 1 || !strings.Contains(ps[0].Message, `agent "gemini" is allowed but not defined`) {
		t.Errorf("unknown agent: %v", ps)
	}
}

// Judge reports a sidecar that does not parse as one problem, and none for
// a change with no sidecar.
func TestJudge(t *testing.T) {
	cfg := loadCfg(t, resolveRepo, "")
	if _, ps := Judge(cfg, nil); len(ps) != 0 {
		t.Errorf("no sidecar: %v", ps)
	}
	if _, ps := Judge(cfg, []byte("[steps.build\n")); len(ps) != 1 {
		t.Errorf("broken sidecar: %v", ps)
	}
	if _, ps := Judge(cfg, []byte("[steps.build]\nagent = \"gemini\"\n")); len(ps) != 1 || ps[0].Key != "steps.build.agent" {
		t.Errorf("not allowed: %v", ps)
	}
}
