// Package testenv builds the stand-in programs the runs tests use (a fake
// agent, a stub openspec CLI and, optionally, tuckerbox itself) and makes
// scratch git repositories holding OpenSpec changes. No real agent or
// openspec CLI is used.
package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Tools are the built test programs.
type Tools struct {
	// Dir holds everything below; Remove deletes it.
	Dir string
	// FakeAgent is the fake agent binary (see testdata/fakeagent).
	FakeAgent string
	// Bin is a directory holding an "openspec" stub, to put first on PATH.
	Bin string
	// Tuckerbox is the tuckerbox binary, if it was built.
	Tuckerbox string
	// HCBin is a directory holding a fake herdr-core "hc" (see
	// testdata/fakehc). It is not on PATH unless a test puts it there.
	HCBin string
}

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func build(out, pkg string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = here()
	// The test programs need no cgo, whatever the race detector needs.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building %s: %v\n%s", pkg, err, b)
	}
	return nil
}

// Build builds the fake agent and openspec stub, and tuckerbox when
// withTuckerbox is set. Call it from TestMain and Remove the result after.
func Build(withTuckerbox bool) (*Tools, error) {
	for k, v := range NoNetwork {
		os.Setenv(k, v)
	}
	dir, err := os.MkdirTemp("", "tuckerbox-testenv")
	if err != nil {
		return nil, err
	}
	t := &Tools{Dir: dir, FakeAgent: filepath.Join(dir, exe("fakeagent")), Bin: filepath.Join(dir, "bin"), HCBin: filepath.Join(dir, "hcbin")}
	steps := [][2]string{
		{t.FakeAgent, "./testdata/fakeagent"},
		{filepath.Join(t.Bin, exe("openspec")), "./testdata/fakeopenspec"},
		{filepath.Join(t.HCBin, exe("hc")), "./testdata/fakehc"},
	}
	if withTuckerbox {
		t.Tuckerbox = filepath.Join(dir, exe("tuckerbox"))
		steps = append(steps, [2]string{t.Tuckerbox, "github.com/chrisjoyce911/tuckerbox/cmd/tuckerbox"})
	}
	for _, s := range steps {
		if err := build(s[0], s[1]); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	return t, nil
}

// NoNetwork is the environment every test git call runs with, set by Build
// (for the whole test process and what it starts), Setup and Git: git may
// use local repositories only (GIT_ALLOW_PROTOCOL=file), and never
// prompts for credentials or host keys.
var NoNetwork = map[string]string{
	"GIT_ALLOW_PROTOCOL":  "file",
	"GIT_TERMINAL_PROMPT": "0",
	"GIT_ASKPASS":         "echo",
	"SSH_ASKPASS":         "echo",
	"GCM_INTERACTIVE":     "never",
	"GIT_SSH_COMMAND":     "ssh -o BatchMode=yes",
}

// PathWithout returns the PATH list path without the directories that hold
// a program called name, so a test can run as on a machine without it.
func PathWithout(path, name string) string {
	var keep []string
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, exe(name))); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			continue
		}
		keep = append(keep, dir)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

// Remove deletes the built programs.
func (t *Tools) Remove() { os.RemoveAll(t.Dir) }

// Env is a test's isolated environment.
type Env struct {
	// Home is a scratch directory for the user state and config dirs.
	Home string
	// StateHome is XDG_STATE_HOME (the machine index lives under it).
	StateHome string
	// Plan is the fake agent's plan file; Record lists its starts.
	Plan, Record string
	// HCDir is the fake hc's directory of terminals (FAKEHC_DIR).
	HCDir string
	// HC is the fake hc program.
	HC string
}

// Setup points PATH at the openspec stub and the user state and config
// directories, the fake agent's plan and its record at scratch paths, for
// the rest of the test (t.Setenv).
func (tl *Tools) Setup(t *testing.T) *Env {
	t.Helper()
	home := t.TempDir()
	e := &Env{
		Home:      home,
		StateHome: filepath.Join(home, "state"),
		Plan:      filepath.Join(home, "plan.json"),
		Record:    filepath.Join(home, "record.txt"),
	}
	// The fake hc comes before any real herdr-core, with its server down,
	// so agent_backend "auto" runs agents headless unless a test sets
	// FAKEHC_SERVER=up.
	sep := string(os.PathListSeparator)
	t.Setenv("PATH", tl.Bin+sep+tl.HCBin+sep+os.Getenv("PATH"))
	t.Setenv("FAKEHC_SERVER", "down")
	e.HCDir = filepath.Join(home, "hc")
	e.HC = filepath.Join(tl.HCBin, exe("hc"))
	t.Setenv("FAKEHC_DIR", e.HCDir)
	t.Cleanup(func() {
		cmd := exec.Command(e.HC, "killall")
		cmd.Env = append(os.Environ(), "FAKEHC_DIR="+e.HCDir)
		_ = cmd.Run()
	})
	for k, v := range NoNetwork {
		t.Setenv(k, v)
	}
	t.Setenv("XDG_STATE_HOME", e.StateHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("FAKEAGENT_PLAN", e.Plan)
	t.Setenv("FAKEAGENT_RECORD", e.Record)
	e.SetPlan(t, map[string]Action{})
	return e
}

// Action is what the fake agent does for a step (see testdata/fakeagent).
type Action struct {
	Status     string    `json:"status,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Findings   []Finding `json:"findings,omitempty"`
	WaitFor    string    `json:"wait_for,omitempty"`
	Touch      string    `json:"touch,omitempty"`
	Exit       int       `json:"exit,omitempty"`
	StaleFirst bool      `json:"stale_first,omitempty"`
	Message    bool      `json:"message,omitempty"`
	// Tick ticks these task numbers in the change's tasks.md in the
	// worktree before reporting.
	Tick []string `json:"tick,omitempty"`
	// Edit appends a line to this file (relative to the change folder in
	// the worktree) before reporting.
	Edit string `json:"edit,omitempty"`
	// Ask, in a terminal, ends the first turn with this question and no
	// result, then reads the person's answer from the terminal; the
	// result's summary then quotes it ("answer: <line>").
	Ask string `json:"ask,omitempty"`
	// ExitAfter, in a terminal, exits once the result's turn has ended,
	// instead of waiting for more input as an interactive CLI does.
	ExitAfter bool `json:"exit_after,omitempty"`
	// FileFirst, in a terminal, writes the result file mid-turn and ends
	// that turn a second later with a message holding no result.
	FileFirst bool `json:"file_first,omitempty"`
	// Trust, in a terminal, first shows Claude Code's folder-trust prompt
	// for this folder ("." is the agent's working directory) and exits
	// unless it is answered with down, then enter.
	Trust string `json:"trust,omitempty"`
}

// Finding is a fake agent finding.
type Finding struct {
	Severity string `json:"severity"`
	File     string `json:"file"`
	Detail   string `json:"detail"`
}

// SetPlan writes the fake agent's plan, keyed "<change>:<step>:<round>",
// "<change>:<step>", "<step>:<round>", "<step>" or "*".
func (e *Env) SetPlan(t *testing.T, plan map[string]Action) {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.Plan, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Starts returns the fake agent's starts so far, as "<change> <step> <round>".
func (e *Env) Starts(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.Record)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 {
			out = append(out, strings.Join(f[:3], " "))
		}
	}
	return out
}

// RepoOptions shape a scratch repository.
type RepoOptions struct {
	// Changes are the OpenSpec change ids to commit.
	Changes []string
	// MaxParallel is max_parallel (default 1).
	MaxParallel int
	// Extra is appended to the config.
	Extra string
	// NoConfig leaves out .tuckerbox/config.toml.
	NoConfig bool
	// RequireApproval leaves require_approval at its default (true). By
	// default the scratch config sets require_approval = false, so tests
	// of other behaviour need not approve their changes.
	RequireApproval bool
	// Interactive gives the fake agent an interactive command, so it runs
	// in a terminal under the terminal backend.
	Interactive bool
}

// Git runs git in dir and fails the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	for k, v := range NoNetwork {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// WriteFile writes a file, making its directory.
func WriteFile(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Config is the workflow the scratch repositories use: build, then review
// looping back to build, both run by the fake agent.
func (tl *Tools) Config(maxParallel int, extra string) string {
	if maxParallel == 0 {
		maxParallel = 1
	}
	return fmt.Sprintf(`max_parallel = %d
ship = "local"

[agents.fake]
command = ['%s', "{brief}"]

[workflow]
steps = ["build", "review"]

[steps.build]
agent = "fake"
role = "builder"

[steps.review]
agent = "fake"
role = "reviewer"
on_reject = "build"
max_rounds = 3
%s`, maxParallel, tl.FakeAgent, extra)
}

// Interactive is the fake agent's interactive command, as a config line:
// it reports through the turn hook it is given and reads the person's
// answers from its terminal.
func (tl *Tools) Interactive() string {
	return fmt.Sprintf(`interactive = ['%s', "--notify", "{turnhook}", "--", "{brief}"]`, tl.FakeAgent)
}

// NewRepo makes a git repository on branch main, with one commit holding
// the config and the changes, in a fresh directory named "repo" (so the
// default worktrees go to a sibling "repo-worktrees").
func (tl *Tools) NewRepo(t *testing.T, o RepoOptions) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks (macOS /var -> /private/var) so paths compare.
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	Git(t, repo, "init", "-q", "-b", "main")
	Git(t, repo, "config", "user.name", "Test")
	Git(t, repo, "config", "user.email", "test@example.invalid")
	Git(t, repo, "config", "commit.gpgsign", "false")
	Git(t, repo, "config", "core.autocrlf", "false")
	WriteFile(t, filepath.Join(repo, "README.md"), "# scratch\n")
	for _, c := range o.Changes {
		AddChange(t, repo, c)
	}
	if !o.NoConfig {
		cfg := tl.Config(o.MaxParallel, o.Extra)
		if o.Interactive {
			cfg = strings.Replace(cfg, "[agents.fake]\n", "[agents.fake]\n"+tl.Interactive()+"\n", 1)
		}
		if !o.RequireApproval {
			cfg = "require_approval = false\n" + cfg
		}
		WriteFile(t, filepath.Join(repo, ".tuckerbox", "config.toml"), cfg)
	}
	Git(t, repo, "add", "-A")
	Git(t, repo, "commit", "-q", "-m", "scratch repository")
	return repo
}

// AddChange writes a minimal OpenSpec change (not committed).
func AddChange(t *testing.T, repo, id string) {
	t.Helper()
	dir := filepath.Join(repo, "openspec", "changes", id)
	WriteFile(t, filepath.Join(dir, "proposal.md"), "# "+id+"\n\n## Why\n\nTest.\n")
	WriteFile(t, filepath.Join(dir, "tasks.md"), "# Tasks\n\n- [ ] 1.1 Do it\n")
}
