package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

var (
	fakeEditorOnce sync.Once
	fakeEditorPath string
	fakeEditorErr  error
)

// fakeEditor builds internal/edits/testdata/fakeeditor once.
func fakeEditor(t *testing.T) string {
	t.Helper()
	fakeEditorOnce.Do(func() {
		name := "fakeeditor"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		fakeEditorPath = filepath.Join(tools.Dir, name)
		cmd := exec.Command("go", "build", "-o", fakeEditorPath, "../edits/testdata/fakeeditor")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeEditorErr = &exec.Error{Name: "go build fakeeditor: " + string(out), Err: err}
		}
	})
	if fakeEditorErr != nil {
		t.Fatal(fakeEditorErr)
	}
	return fakeEditorPath
}

// setEditor writes machine config whose editor is the fake editor with
// flags, and clears $VISUAL and $EDITOR.
func (f *cliFixture) setEditor(flags ...string) {
	f.t.Helper()
	cmd := "'" + fakeEditor(f.t) + "'"
	for _, fl := range flags {
		cmd += " '" + fl + "'"
	}
	f.t.Setenv("VISUAL", "")
	f.t.Setenv("EDITOR", "")
	testenv.WriteFile(f.t, filepath.Join(f.env.Home, "config", "tuckerbox", "config.toml"),
		"editor = "+strconv.Quote(cmd)+"\n")
}

func (f *cliFixture) worktree(change string) string {
	return filepath.Join(filepath.Dir(f.repo), "repo-worktrees", change)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Scenario: Change without a worktree; then Change with a worktree.
func TestEditWhereEditsHappen(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	opened := filepath.Join(f.env.Home, "opened")
	f.setEditor("--opened", opened, "--append", "edited\n")

	res := f.tb("edit", "add-login", "design")
	if res.code != 0 {
		t.Fatalf("edit: %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	inCheckout := filepath.Join(f.repo, "openspec", "changes", "add-login", "design.md")
	if got := readFile(t, opened); !sameFile(got, inCheckout) {
		t.Errorf("opened %s, want %s", got, inCheckout)
	}
	for _, s := range []string{"repository checkout " + f.repo, "is valid", "not committed"} {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("output lacks %q:\n%s", s, res.stdout)
		}
	}
	if st := testenv.Git(t, f.repo, "status", "--porcelain"); !strings.Contains(st, "design.md") {
		t.Errorf("design.md not left uncommitted: %q", st)
	}
	if n := testenv.Git(t, f.repo, "rev-list", "--count", "HEAD"); n != "1" {
		t.Errorf("edit committed: %s commits", n)
	}

	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %s", res.stderr)
	}
	res = f.tb("edit", "add-login", "design")
	if res.code != 0 {
		t.Fatalf("edit with worktree: %d %s", res.code, res.stderr)
	}
	inWorktree := filepath.Join(f.worktree("add-login"), "openspec", "changes", "add-login", "design.md")
	if got := readFile(t, opened); !sameFile(got, inWorktree) {
		t.Errorf("opened %s, want the worktree's %s", got, inWorktree)
	}
	if !strings.Contains(res.stdout, "change's worktree") {
		t.Errorf("folder not named: %s", res.stdout)
	}
}

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// Scenario: No editor set.
func TestEditNoEditor(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	res := f.tb("edit", "add-login", "design")
	if res.code == 0 || !strings.Contains(res.stderr, "no editor is set") || !strings.Contains(res.stderr, "$VISUAL") {
		t.Fatalf("%d %s", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "openspec", "changes", "add-login", "design.md")); err == nil {
		t.Error("a file was made")
	}
	// $EDITOR is used when nothing else is set.
	t.Setenv("EDITOR", "'"+fakeEditor(t)+"' --append x")
	if res := f.tb("edit", "add-login", "design"); res.code != 0 {
		t.Errorf("with $EDITOR: %d %s", res.code, res.stderr)
	}
}

// The repository can't set the editor.
func TestEditRepoEditorRefused(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	cfg := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	testenv.WriteFile(t, cfg, "editor = \"vim\"\n"+readFile(t, cfg))
	res := f.tb("edit", "add-login", "design")
	if res.code == 0 || !strings.Contains(res.stderr, "editor is machine-only") {
		t.Fatalf("%d %s", res.code, res.stderr)
	}
}

// Scenario: Edit breaks a spec. The openspec stub reports INVALID's text as
// the strict validation error (the real output is parsed in
// internal/edits).
func TestEditBreaksSpec(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "specs", "auth", "spec.md"), "x\n")
	f.setEditor("--invalid", `auth/spec.md: ADDED "Login" must include at least one scenario`)
	res := f.tb("edit", "add-login", "spec:auth")
	if res.code == 0 || !strings.Contains(res.stdout, `"Login" must include at least one scenario`) || !strings.Contains(res.stdout, "not valid") {
		t.Fatalf("%d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Unknown spec.
func TestEditUnknownSpec(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "specs", "auth", "spec.md"), "x\n")
	opened := filepath.Join(f.env.Home, "opened")
	f.setEditor("--opened", opened)
	res := f.tb("edit", "add-login", "spec:billing")
	if res.code == 0 || !strings.Contains(res.stderr, "no billing spec delta") || !strings.Contains(res.stderr, "auth") {
		t.Fatalf("%d %s", res.code, res.stderr)
	}
	if _, err := os.Stat(opened); err == nil {
		t.Error("the editor was opened")
	}
	if res := f.tb("edit", "add-login", "readme"); res.code != 2 {
		t.Errorf("unknown file kind: %d %s", res.code, res.stderr)
	}
}

// Scenario: Valid change, as a dashboard reads it.
func TestValidateJSON(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	res := f.tb("validate", "add-login", "--json")
	if res.code != 0 {
		t.Fatalf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	var doc struct {
		Version *int              `json:"version"`
		Valid   bool              `json:"valid"`
		Errors  []json.RawMessage `json:"errors"`
		Folder  string            `json:"folder"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatalf("%v\n%s", err, res.stdout)
	}
	if doc.Version == nil || *doc.Version != 1 || !doc.Valid || doc.Errors == nil || len(doc.Errors) != 0 || doc.Folder != f.repo {
		t.Errorf("document %s", res.stdout)
	}
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "INVALID"), "broken\n")
	res = f.tb("validate", "add-login", "--json")
	if res.code != 1 || !strings.Contains(res.stdout, `"valid": false`) || !strings.Contains(res.stdout, "broken") {
		t.Errorf("invalid: %d %s", res.code, res.stdout)
	}
	if res := f.tb("validate", "add-login"); res.code != 1 || !strings.Contains(res.stdout, "broken") {
		t.Errorf("text: %d %s", res.code, res.stdout)
	}
}

// Scenarios: Tick a task; Unknown task.
func TestTasksTick(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	tasks := filepath.Join(f.repo, "openspec", "changes", "add-login", "tasks.md")
	testenv.WriteFile(t, tasks, "# Tasks\r\n\r\n- [ ] 1.1 One\r\n- [ ] 2.1 Two\r\n")
	res := f.tb("tasks", "add-login", "tick", "2.1")
	if res.code != 0 || !strings.Contains(res.stdout, "not committed") || !strings.Contains(res.stdout, f.repo) {
		t.Fatalf("tick: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if got := readFile(t, tasks); got != "# Tasks\r\n\r\n- [ ] 1.1 One\r\n- [x] 2.1 Two\r\n" {
		t.Errorf("tasks.md %q", got)
	}
	if res := f.tb("tasks", "add-login", "tick", "2.1"); res.code != 0 || !strings.Contains(res.stdout, "already ticked") {
		t.Errorf("tick again: %d %s", res.code, res.stdout)
	}
	if res := f.tb("tasks", "add-login", "untick", "2.1"); res.code != 0 {
		t.Errorf("untick: %d %s", res.code, res.stderr)
	}
	if got := readFile(t, tasks); got != "# Tasks\r\n\r\n- [ ] 1.1 One\r\n- [ ] 2.1 Two\r\n" {
		t.Errorf("after untick %q", got)
	}
	res = f.tb("tasks", "add-login", "tick", "9.9")
	if res.code == 0 || !strings.Contains(res.stderr, "no task 9.9 exists") {
		t.Errorf("unknown task: %d %s", res.code, res.stderr)
	}
	if res := f.tb("tasks", "add-login", "flip", "1.1"); res.code != 2 {
		t.Errorf("bad verb: %d", res.code)
	}
}

// Scenario: Tick during a build step, against a real run process.
func TestTasksRefusedDuringRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "build")
	tasks := filepath.Join(f.worktree("add-login"), "openspec", "changes", "add-login", "tasks.md")
	before := readFile(t, tasks)
	res := f.tb("tasks", "add-login", "tick", "1.1")
	if res.code == 0 || !strings.Contains(res.stderr, "running step build") {
		t.Fatalf("tick during build: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if readFile(t, tasks) != before {
		t.Error("tasks.md changed")
	}
	f.setEditor("--append", "x")
	if res := f.tb("edit", "add-login", "design"); res.code == 0 || !strings.Contains(res.stderr, "running step build") {
		t.Errorf("edit during build: %d %s", res.code, res.stderr)
	}
	f.open("build")
	f.waitState("add-login", "ready", "")
	if res := f.tb("tasks", "add-login", "tick", "1.1"); res.code != 0 {
		t.Errorf("tick after the run: %d %s", res.code, res.stderr)
	}
}

// Scenario: Resume while the editor is open. A run and a rerun are refused
// the same way, and the edit itself is unaffected.
func TestEditBlocksRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"build:1": {WaitFor: f.gate("never")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "build")
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: %d %s", res.code, res.stderr)
	}

	opened := filepath.Join(f.env.Home, "opened")
	closeGate := filepath.Join(f.env.Home, "close-editor")
	f.setEditor("--opened", opened, "--wait-for", closeGate, "--append", "edited\n")
	done := make(chan cmdResult, 1)
	go func() { done <- tbIn(t, f.repo, "edit", "add-login", "design") }()
	t.Cleanup(func() { testenv.WriteFile(t, closeGate, "go\n") })
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(opened); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the editor did not open")
		}
		time.Sleep(20 * time.Millisecond)
	}

	for _, args := range [][]string{{"resume", "add-login"}, {"run", "add-login"}, {"rerun", "add-login", "build"}} {
		res := f.tb(args...)
		if res.code == 0 || !strings.Contains(res.stderr, "is being edited") || !strings.Contains(res.stderr, "tuckerbox edit design") {
			t.Errorf("%v: %d %s %s", args, res.code, res.stdout, res.stderr)
		}
	}
	if s, _ := f.state("add-login"); s != "stopped" {
		t.Errorf("state %s while editing", s)
	}
	if res := f.tb("tasks", "add-login", "tick", "1.1"); res.code == 0 || !strings.Contains(res.stderr, "being edited") {
		t.Errorf("tick while editing: %d %s", res.code, res.stderr)
	}

	testenv.WriteFile(t, closeGate, "go\n")
	res := <-done
	if res.code != 0 || !strings.Contains(res.stdout, "change's worktree") {
		t.Fatalf("edit: %d %s %s", res.code, res.stdout, res.stderr)
	}
	// The lock is free again: resume runs to the end.
	f.env.SetPlan(t, map[string]testenv.Action{})
	if res := f.tb("resume", "add-login"); res.code != 0 {
		t.Fatalf("resume after the edit: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}

// Scenario: Edit a blocked run.
func TestEditBlockedRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review": {Status: "rejected", Summary: "not yet"}})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "blocked", "")
	opened := filepath.Join(f.env.Home, "opened")
	f.setEditor("--opened", opened, "--append", "fixed\n")
	if res := f.tb("edit", "add-login", "design"); res.code != 0 {
		t.Fatalf("edit: %d %s %s", res.code, res.stdout, res.stderr)
	}
	if _, err := os.Stat(opened); err != nil {
		t.Error("the editor did not open")
	}
}
