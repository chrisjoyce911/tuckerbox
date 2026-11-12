package config

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const minimal = `
[workflow]
steps = ["plan", "build", "review"]

[steps.plan]
agent = "claude"
role = "planner"

[steps.build]
agent = "codex"
role = "builder"

[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"
`

type fixture struct {
	t       *testing.T
	root    string
	machine string
	remote  string
}

func newFixture(t *testing.T, repo, machine string) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir()}
	if repo != "" {
		writeFile(t, filepath.Join(f.root, RepoFile), repo)
	}
	f.machine = filepath.Join(t.TempDir(), "tuckerbox", "config.toml")
	if machine != "" {
		writeFile(t, f.machine, machine)
	}
	return f
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) load() (*Config, error) {
	return Load(Options{RepoRoot: f.root, MachinePath: f.machine, Remote: f.remote})
}

// mustLoad loads and fails the test on any error.
func (f *fixture) mustLoad() *Config {
	f.t.Helper()
	cfg, err := f.load()
	if err != nil {
		f.t.Fatalf("Load: %v", err)
	}
	return cfg
}

// problems loads, expects Problems and returns them.
func (f *fixture) problems() Problems {
	f.t.Helper()
	_, err := f.load()
	var ps Problems
	if !errors.As(err, &ps) {
		f.t.Fatalf("Load: want Problems, got %v", err)
	}
	return ps
}

// wantProblem checks that one problem has the key and every fragment in
// its message, and that it names the file.
func wantProblem(t *testing.T, ps Problems, file, key string, fragments ...string) {
	t.Helper()
	for _, p := range ps {
		if p.Key != key || p.File != file {
			continue
		}
		ok := true
		for _, frag := range fragments {
			if !strings.Contains(p.Message, frag) {
				ok = false
			}
		}
		if ok {
			if !strings.HasPrefix(p.String(), file) || !strings.Contains(p.String(), key) {
				t.Errorf("String() %q lacks file or key", p.String())
			}
			return
		}
	}
	t.Errorf("no problem in %s at %s containing %q; got:\n%v", file, key, fragments, ps)
}

func (f *fixture) repoPath() string { return filepath.Join(f.root, RepoFile) }

// Scenario: Config present.
func TestConfigPresent(t *testing.T) {
	cfg := newFixture(t, minimal, "").mustLoad()
	var ids []string
	for _, s := range cfg.Steps {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"plan", "build", "review"}) {
		t.Errorf("steps %v", ids)
	}
	review, _ := cfg.Step("review")
	if review.Agent != "claude" || review.Role != "reviewer" || review.OnReject != "build" {
		t.Errorf("review %+v", review)
	}
	if review.MaxRounds != DefaultMaxRounds {
		t.Errorf("max_rounds %d, want default %d", review.MaxRounds, DefaultMaxRounds)
	}
	if cfg.MaxParallel != DefaultMaxParallel || cfg.Ship != ShipLocal {
		t.Errorf("defaults: max_parallel %d ship %q", cfg.MaxParallel, cfg.Ship)
	}
	if cfg.Worktree.Path != DefaultWorktreePath || cfg.Worktree.Branch != DefaultBranch || cfg.Worktree.Base != "" {
		t.Errorf("worktree %+v", cfg.Worktree)
	}
	if cfg.MachinePath != "" {
		t.Errorf("machine path %q with no machine file", cfg.MachinePath)
	}
}

// Scenario: Config absent.
func TestConfigAbsent(t *testing.T) {
	_, err := newFixture(t, "", minimal).load()
	if !errors.Is(err, ErrNoRepoConfig) {
		t.Fatalf("err %v, want ErrNoRepoConfig", err)
	}
	if !strings.Contains(err.Error(), "tuckerbox init") {
		t.Errorf("message does not say how to create one: %v", err)
	}
}

// Scenario: Repo overrides a default.
func TestRepoOverridesMachine(t *testing.T) {
	cfg := newFixture(t, "max_parallel = 2\n"+minimal, "max_parallel = 4\n").mustLoad()
	if cfg.MaxParallel != 2 {
		t.Errorf("max_parallel %d, want 2", cfg.MaxParallel)
	}
	cfg = newFixture(t, minimal, "max_parallel = 4\n").mustLoad()
	if cfg.MaxParallel != 4 {
		t.Errorf("machine default not used: max_parallel %d, want 4", cfg.MaxParallel)
	}
}

func TestStepsMergePerKey(t *testing.T) {
	machine := `
editor = "code --wait"
[workflow]
steps = ["build"]
[steps.build]
agent = "codex"
role = "builder"
model = "gpt-x"
effort = "high"
[steps.spare]
agent = "claude"
role = "planner"
`
	repo := `
[workflow]
steps = ["build"]
[steps.build]
agent = "claude"
`
	cfg := newFixture(t, repo, machine).mustLoad()
	b, _ := cfg.Step("build")
	if b.Agent != "claude" || b.Role != "builder" || b.Model != "gpt-x" || b.Effort != "high" {
		t.Errorf("build %+v", b)
	}
	if cfg.Editor != "code --wait" {
		t.Errorf("editor %q", cfg.Editor)
	}
}

func TestWorkflowFromMachine(t *testing.T) {
	cfg := newFixture(t, "max_parallel = 1\n", minimal).mustLoad()
	if len(cfg.Steps) != 3 {
		t.Errorf("steps %+v", cfg.Steps)
	}
}

// Scenario: Repository asks for an undefined profile.
func TestUndefinedProfile(t *testing.T) {
	repo := strings.Replace(minimal, `role = "builder"`, "role = \"builder\"\npermissions = \"unattended\"", 1)
	f := newFixture(t, repo, "")
	wantProblem(t, f.problems(), f.repoPath(), "steps.build.permissions", `"unattended"`, "not defined in machine config")
}

func TestProfileAllowedRemotes(t *testing.T) {
	repo := strings.Replace(minimal, `role = "builder"`, "role = \"builder\"\npermissions = \"edit\"", 1)
	machine := `
[permissions.edit]
allowed_remotes = ["git@github.com:me/allowed.git", "gitlab.example.com/team/*"]
[permissions.edit.args]
codex = ["--sandbox", "workspace-write"]
`
	for _, c := range []struct {
		remote string
		ok     bool
	}{
		{"https://github.com/me/allowed", true},
		{"ssh://git@GitHub.com/me/allowed.git", true},
		{"git@gitlab.example.com:team/thing.git", true},
		{"git@github.com:someone/else.git", false},
		{"", false},
	} {
		f := newFixture(t, repo, machine)
		f.remote = c.remote
		cfg, err := f.load()
		if c.ok {
			if err != nil {
				t.Errorf("%q: %v", c.remote, err)
				continue
			}
			b, _ := cfg.Step("build")
			if got := cfg.PermissionArgs(b); !slices.Equal(got, []string{"--sandbox", "workspace-write"}) {
				t.Errorf("%q: args %v", c.remote, got)
			}
			continue
		}
		var ps Problems
		if !errors.As(err, &ps) {
			t.Errorf("%q: want problems, got %v", c.remote, err)
			continue
		}
		wantProblem(t, ps, f.repoPath(), "steps.build.permissions", "does not allow", "allowed_remotes")
	}
}

func TestProfileStar(t *testing.T) {
	repo := strings.Replace(minimal, `role = "builder"`, "role = \"builder\"\npermissions = \"edit\"", 1)
	machine := `
[permissions.edit]
allowed_remotes = ["*"]
args = { codex = ["--full-auto"] }
`
	cfg := newFixture(t, repo, machine).mustLoad()
	plan, _ := cfg.Step("plan")
	if args := cfg.PermissionArgs(plan); args != nil {
		t.Errorf("step with no profile got args %v", args)
	}
}

func TestProfileMissingAgentArgs(t *testing.T) {
	repo := strings.Replace(minimal, `role = "builder"`, "role = \"builder\"\npermissions = \"edit\"", 1)
	machine := `
[permissions.edit]
allowed_remotes = ["*"]
args = { claude = ["--permission-mode", "acceptEdits"] }
`
	f := newFixture(t, repo, machine)
	wantProblem(t, f.problems(), f.repoPath(), "steps.build.permissions", `no flags for agent "codex"`)
}

func TestProfileValidation(t *testing.T) {
	machine := `
[permissions.empty]
[permissions.odd]
allowed_remotes = ["*"]
args = { nosuch = ["-x"] }
`
	f := newFixture(t, minimal, machine)
	ps := f.problems()
	wantProblem(t, ps, f.machine, "permissions.empty.allowed_remotes", "missing")
	wantProblem(t, ps, f.machine, "permissions.empty.args", "missing")
	wantProblem(t, ps, f.machine, "permissions.odd.args.nosuch", "unknown agent")
}

// Scenario: Repository puts flags in its config.
func TestRepoRawArgs(t *testing.T) {
	for _, extra := range []string{
		"[steps.build]\nagent = \"codex\"\nrole = \"builder\"\nargs = [\"--yolo\"]\n",
		"[agents.omp]\ncommand = [\"omp\", \"{brief}\"]\nargs = [\"--fast\"]\n",
	} {
		repo := strings.Replace(minimal, "[steps.build]\nagent = \"codex\"\nrole = \"builder\"\n", "", 1) + extra
		f := newFixture(t, repo, "")
		ps := f.problems()
		found := false
		for _, p := range ps {
			if p.File == f.repoPath() && strings.HasSuffix(p.Key, ".args") && strings.Contains(p.Message, "belong in a permission profile in machine config") {
				found = true
			}
		}
		if !found {
			t.Errorf("no raw-args problem for:\n%s\ngot:\n%v", extra, ps)
		}
	}
}

func TestRepoMachineOnlyKeys(t *testing.T) {
	repo := "editor = \"vim\"\n" + minimal + `
[permissions.edit]
allowed_remotes = ["*"]
args = { claude = ["--dangerously-skip-permissions"] }
`
	f := newFixture(t, repo, "")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "editor", "machine-only")
	wantProblem(t, ps, f.repoPath(), "permissions", "machine-only")
	for _, p := range ps {
		if strings.HasPrefix(p.Key, "permissions.") {
			t.Errorf("profile contents reported separately: %v", p)
		}
	}
}

func TestRepoAgentBypassFlag(t *testing.T) {
	repo := strings.Replace(minimal, `agent = "codex"`, `agent = "omp"`, 1) + `
[agents.omp]
command = ["omp", "--yolo", "--permission-mode=bypass", "{brief}"]
`
	f := newFixture(t, repo, "")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "agents.omp.command", "--yolo", "machine config")
	wantProblem(t, ps, f.repoPath(), "agents.omp.command", "--permission-mode", "machine config")
}

func TestRepoCannotRedefineBuiltin(t *testing.T) {
	f := newFixture(t, minimal+"\n[agents.claude]\ncommand = [\"claude\", \"{brief}\"]\n", "")
	wantProblem(t, f.problems(), f.repoPath(), "agents.claude", "built-in")
}

// Custom agent: defined in config with a template, used by a step.
func TestCustomAgent(t *testing.T) {
	repo := strings.Replace(minimal, `agent = "codex"`, `agent = "omp"`, 1) + `
[agents.omp]
command = ["omp", "run", "--brief={brief}", "--cwd", "{workdir}", "--model", "{model}", "-o", "{lastmessage}"]
model = "big"
`
	machine := `
[agents.claude]
command = ["claude", "-p", "{brief}", "--model", "{model}"]
[agents.helper]
command = ["helper", "{brief}"]
`
	cfg := newFixture(t, repo, machine).mustLoad()
	omp := cfg.Agents["omp"]
	if !omp.FromRepo || omp.Model != "big" || omp.Command[0] != "omp" {
		t.Errorf("omp %+v", omp)
	}
	if c := cfg.Agents["claude"]; c.FromRepo || len(c.Command) != 5 {
		t.Errorf("machine claude override %+v", c)
	}
	if _, ok := cfg.Agents["codex"]; ok {
		t.Error("built-in codex without override should not be in Agents")
	}
}

func TestAgentTemplateValidation(t *testing.T) {
	repo := strings.Replace(minimal, `agent = "codex"`, `agent = "a"`, 1) + `
[agents.a]
command = ["a", "{workdir}", "{nope}"]
[agents.b]
model = "x"
`
	f := newFixture(t, repo, "")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "agents.a.command", "{nope}")
	wantProblem(t, ps, f.repoPath(), "agents.a.command", "{brief}")
	wantProblem(t, ps, f.repoPath(), "agents.b.command", "missing")
}

// Scenario: Loop to an undefined step.
func TestLoopToUndefinedStep(t *testing.T) {
	f := newFixture(t, strings.Replace(minimal, `on_reject = "build"`, `on_reject = "bulid"`, 1), "")
	wantProblem(t, f.problems(), f.repoPath(), "steps.review.on_reject", `"bulid"`, "unknown step")
}

func TestStepValidation(t *testing.T) {
	repo := `
max_parallel = 0
ship = "unknown"
[workflow]
steps = ["plan", "build", "plan", "review"]
[steps.plan]
agent = "gemini"
role = "dreamer"
on_reject = "review"
[steps.build]
role = "builder"
max_rounds = 2
[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"
max_rounds = 0
[steps.orphan]
agent = "claude"
role = "planner"
`
	f := newFixture(t, repo, "")
	ps := f.problems()
	rp := f.repoPath()
	wantProblem(t, ps, rp, "max_parallel", "at least 1")
	wantProblem(t, ps, rp, "ship", "unknown ship mode")
	wantProblem(t, ps, rp, "workflow.steps", `"plan" is listed twice`)
	wantProblem(t, ps, rp, "steps.plan.agent", `unknown agent "gemini"`)
	wantProblem(t, ps, rp, "steps.plan.role", `unknown role "dreamer"`)
	wantProblem(t, ps, rp, "steps.plan.on_reject", "comes after")
	wantProblem(t, ps, rp, "steps.build.agent", "missing")
	wantProblem(t, ps, rp, "steps.build.max_rounds", "without on_reject")
	wantProblem(t, ps, rp, "steps.review.max_rounds", "at least 1")
	wantProblem(t, ps, rp, "steps.orphan", "not in workflow.steps")
}

func TestNoWorkflow(t *testing.T) {
	f := newFixture(t, "max_parallel = 1\n", "")
	wantProblem(t, f.problems(), f.repoPath(), "workflow.steps", "no steps")
	f = newFixture(t, "[workflow]\nsteps = []\n", minimal)
	wantProblem(t, f.problems(), f.repoPath(), "workflow.steps", "no steps")
}

func TestRoleOverrideFile(t *testing.T) {
	f := newFixture(t, strings.Replace(minimal, `role = "planner"`, `role = "architect"`, 1), "")
	writeFile(t, filepath.Join(f.root, RepoDir, "roles", "architect.md"), "You design.\n")
	plan, _ := f.mustLoad().Step("plan")
	if plan.Role != "architect" {
		t.Errorf("role %q", plan.Role)
	}
}

func TestUnknownKeys(t *testing.T) {
	repo := "max_paralel = 2\n" + strings.Replace(minimal, `role = "planner"`, "role = \"planner\"\ntimeout = 5", 1) + "\n[mystery]\na = 1\nb = 2\n"
	f := newFixture(t, repo, "colour = \"red\"\n")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "max_paralel", "unknown key")
	wantProblem(t, ps, f.repoPath(), "steps.plan.timeout", "unknown key")
	wantProblem(t, ps, f.repoPath(), "mystery", "unknown key")
	wantProblem(t, ps, f.machine, "colour", "unknown key")
	for _, p := range ps {
		if strings.HasPrefix(p.Key, "mystery.") {
			t.Errorf("child of unknown table reported: %v", p)
		}
	}
}

func TestSyntaxAndTypeErrors(t *testing.T) {
	f := newFixture(t, "[workflow\nsteps = 1\n", "")
	ps := f.problems()
	if len(ps) == 0 || ps[0].File != f.repoPath() || ps[0].Line == 0 || len(ps) != 1 {
		t.Errorf("syntax error: %v", ps)
	}
	f = newFixture(t, "max_parallel = \"two\"\n"+minimal, "")
	ps = f.problems()
	if len(ps) == 0 || ps[0].File != f.repoPath() || ps[0].Key != "max_parallel" || ps[0].Line != 1 || len(ps) != 1 {
		t.Errorf("type error: %#v", ps)
	}
}

func TestWorktreeSettings(t *testing.T) {
	cfg := newFixture(t, minimal+"\n[worktree]\nbase = \"develop\"\nbranch = \"tb/{change}\"\n", "[worktree]\npath = \"/wt/{repo}/{change}\"\n").mustLoad()
	want := Worktree{Base: "develop", Path: "/wt/{repo}/{change}", Branch: "tb/{change}"}
	if cfg.Worktree != want {
		t.Errorf("worktree %+v, want %+v", cfg.Worktree, want)
	}
	f := newFixture(t, minimal+"\n[worktree]\npath = \"../wt\"\nbranch = \"{change}-{when}\"\n", "")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "worktree.path", "{change}")
	wantProblem(t, ps, f.repoPath(), "worktree.branch", "{when}")
}

func TestStarterIsValid(t *testing.T) {
	f := newFixture(t, "", "")
	path, err := WriteStarter(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if path != f.repoPath() {
		t.Errorf("path %s", path)
	}
	cfg := f.mustLoad()
	if len(cfg.Steps) != 3 {
		t.Errorf("starter steps %+v", cfg.Steps)
	}
	// The user-visible rules are explained in the file itself.
	for _, frag := range []string{
		"cannot be", "redefined here", "--dangerously-skip-permissions", // repo agents
		"this step or an earlier one", "needs on_reject", "listed in workflow.steps", // loops
		"(default 1)", "default 3", `"../{repo}-worktrees/{change}"`, `branch = "{change}"`, "remote default branch", // defaults
		"$XDG_CONFIG_HOME", "~/.config/tuckerbox", "%AppData%", // machine path
	} {
		if !strings.Contains(Starter, frag) {
			t.Errorf("starter does not mention %q", frag)
		}
	}
}

func TestStarterNeverOverwrites(t *testing.T) {
	f := newFixture(t, "max_parallel = 9\n", "")
	if _, err := WriteStarter(f.root); !errors.Is(err, ErrRepoConfigExists) {
		t.Fatalf("err %v, want ErrRepoConfigExists", err)
	}
	data, _ := os.ReadFile(f.repoPath())
	if string(data) != "max_parallel = 9\n" {
		t.Errorf("file changed: %q", data)
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:Owner/Repo.git":            "github.com/Owner/Repo",
		"https://github.com/owner/repo":            "github.com/owner/repo",
		"https://user:tok@GitHub.com/owner/repo/":  "github.com/owner/repo",
		"ssh://git@github.com:22/owner/repo.git":   "github.com/owner/repo",
		"gitlab.example.com:group/sub/project.git": "gitlab.example.com/group/sub/project",
		"github.com/owner/*":                       "github.com/owner/*",
		"":                                         "",
		"file:///srv/git/repo.git":                 "/srv/git/repo",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
	if isSCP(`C:\repos\x`) {
		t.Error("drive path treated as scp")
	}
}

// change-approval: require_approval defaults to true and is read from the
// repository config only.
func TestRequireApproval(t *testing.T) {
	if cfg := newFixture(t, minimal, "").mustLoad(); !cfg.RequireApproval {
		t.Error("require_approval should default to true")
	}
	if cfg := newFixture(t, "require_approval = false\n"+minimal, "").mustLoad(); cfg.RequireApproval {
		t.Error("repository require_approval = false ignored")
	}
}

// Scenario: Machine config sets the key.
func TestRequireApprovalMachineOnlyRefused(t *testing.T) {
	f := newFixture(t, minimal, "require_approval = false\n")
	wantProblem(t, f.problems(), f.machine, "require_approval", "belongs in repository config")
}

func TestBoardColumns(t *testing.T) {
	for _, tc := range []struct {
		name, repo, machine string
		want                BoardColumns
	}{
		{"default", "", "", BoardCompact},
		{"repo steps", "steps", "", BoardSteps},
		{"repo compact", "compact", "", BoardCompact},
		{"machine default", "", "steps", BoardSteps},
		{"repo override", "compact", "steps", BoardCompact},
		{"reverse override", "steps", "compact", BoardSteps},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, machine := minimal, ""
			if tc.repo != "" {
				repo += "\n[board]\ncolumns = '" + tc.repo + "'\n"
			}
			if tc.machine != "" {
				machine = "[board]\ncolumns = '" + tc.machine + "'\n"
			}
			if got := newFixture(t, repo, machine).mustLoad().BoardColumns; got != tc.want {
				t.Fatalf("%s != %s", got, tc.want)
			}
		})
	}
	for _, machine := range []bool{false, true} {
		for _, value := range []string{"wide", "", "Compact"} {
			repo, mc := minimal, ""
			setting := "\n[board]\ncolumns = '" + value + "'\n"
			if machine {
				mc = setting
			} else {
				repo += setting
			}
			f := newFixture(t, repo, mc)
			_, err := f.load()
			var ps Problems
			if !errors.As(err, &ps) {
				t.Fatalf("expected config problems, got %v", err)
			}
			wantFile := filepath.Join(f.root, RepoFile)
			if machine {
				wantFile = f.machine
			}
			found := false
			for _, p := range ps {
				if p.File == wantFile && p.Key == "board.columns" && strings.Contains(p.Message, "unknown board layout") {
					found = true
				}
			}
			if !found {
				t.Fatal(ps)
			}
		}
	}
}
