package edits

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

const crlfTasks = "# Tasks\r\n\r\n## 1. One\r\n\r\n- [ ] 1.1 First\r\n- [x] 1.2 Second\r\n\r\n## 2. Two\r\n\r\n- [ ] 2.1 Third task\r\n- [X] 2.10 Tenth\r\n  - [ ] 2.2 Indented\r\n```\r\n- [ ] 3.1 In a code block\r\n```\r\n- [ ] 2.3 Last line, no newline"

func writeTasks(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tasks.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Scenario: Tick a task, byte for byte on a CRLF tasks.md.
func TestSetTaskCRLFByteForByte(t *testing.T) {
	p := writeTasks(t, crlfTasks)
	changed, err := SetTask(p, "2.1", true)
	if err != nil || !changed {
		t.Fatalf("tick 2.1: %v %v", changed, err)
	}
	got := []byte(read(t, p))
	want := []byte(strings.Replace(crlfTasks, "- [ ] 2.1 Third", "- [x] 2.1 Third", 1))
	if !bytes.Equal(got, want) {
		t.Fatalf("file differs:\n%q\nwant\n%q", got, want)
	}
	diff := 0
	for i := range got {
		if got[i] != []byte(crlfTasks)[i] {
			diff++
		}
	}
	if diff != 1 || len(got) != len(crlfTasks) {
		t.Errorf("%d bytes changed, length %d → %d", diff, len(crlfTasks), len(got))
	}
}

func TestSetTaskCases(t *testing.T) {
	cases := []struct {
		n       string
		tick    bool
		changed bool
		from    string
		to      string
	}{
		{"1.1", true, true, "- [ ] 1.1", "- [x] 1.1"},
		{"1.2", false, true, "- [x] 1.2", "- [ ] 1.2"},
		{"2.10", false, true, "- [X] 2.10", "- [ ] 2.10"}, // not 2.1
		{"2.2", true, true, "  - [ ] 2.2", "  - [x] 2.2"},
		{"2.3", true, true, "- [ ] 2.3 Last", "- [x] 2.3 Last"},
		{"1.2", true, false, "", ""},  // already ticked
		{"2.10", true, false, "", ""}, // X counts as ticked
		{"1.1", false, false, "", ""}, // already unticked
	}
	for _, c := range cases {
		p := writeTasks(t, crlfTasks)
		changed, err := SetTask(p, c.n, c.tick)
		if err != nil || changed != c.changed {
			t.Errorf("%s tick=%v: changed %v, err %v", c.n, c.tick, changed, err)
			continue
		}
		want := crlfTasks
		if c.changed {
			want = strings.Replace(crlfTasks, c.from, c.to, 1)
		}
		if got := read(t, p); got != want {
			t.Errorf("%s tick=%v: file is\n%q", c.n, c.tick, got)
		}
	}
}

// Scenario: Unknown task.
func TestSetTaskUnknown(t *testing.T) {
	for _, n := range []string{"9.9", "3.1" /* only in a code block */, "2"} {
		p := writeTasks(t, crlfTasks)
		_, err := SetTask(p, n, true)
		if err == nil {
			t.Errorf("%s: no error", n)
		}
		if got := read(t, p); got != crlfTasks {
			t.Errorf("%s: file changed", n)
		}
	}
	p := writeTasks(t, crlfTasks)
	_, err := SetTask(p, "9.9", true)
	var nt *ErrNoTask
	if !errors.As(err, &nt) || !strings.Contains(err.Error(), "no task 9.9 exists") {
		t.Errorf("error %v", err)
	}
	p = writeTasks(t, "- [ ] 1.1 a\n- [ ] 1.1 b\n")
	if _, err := SetTask(p, "1.1", true); err == nil || !strings.Contains(err.Error(), "appears 2 times") {
		t.Errorf("duplicate: %v", err)
	}
}

func TestSplitCommand(t *testing.T) {
	cases := map[string][]string{
		"vim":                       {"vim"},
		"  code   --wait ":          {"code", "--wait"},
		`'/my apps/ed' -w`:          {"/my apps/ed", "-w"},
		`"C:\Program Files\ed.exe"`: {`C:\Program Files\ed.exe`},
		`C:\tools\ed.exe --x`:       {`C:\tools\ed.exe`, "--x"},
		`ed "a \"b\" c"`:            {"ed", `a "b" c`},
		`ed a''b`:                   {"ed", "ab"},
	}
	for in, want := range cases {
		got, err := SplitCommand(in)
		if err != nil || strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "  ", `ed "x`, "ed 'x"} {
		if _, err := SplitCommand(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// Scenario: No editor set. And the order: machine editor, $VISUAL, $EDITOR.
func TestEditorCommand(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if _, err := EditorCommand("", env(nil)); !errors.Is(err, ErrNoEditor) || !strings.Contains(err.Error(), "machine config") {
		t.Errorf("no editor: %v", err)
	}
	all := env(map[string]string{"VISUAL": "visual", "EDITOR": "editor"})
	for _, c := range []struct {
		cfg  string
		env  func(string) string
		want string
	}{
		{"code --wait", all, "code"},
		{"", all, "visual"},
		{"", env(map[string]string{"EDITOR": "nano"}), "nano"},
	} {
		got, err := EditorCommand(c.cfg, c.env)
		if err != nil || got[0] != c.want {
			t.Errorf("%q: %v %v", c.cfg, got, err)
		}
	}
}

func TestParseFile(t *testing.T) {
	for in, rel := range map[string]string{
		"proposal": "proposal.md", "design": "design.md", "model": "model.md", "tasks": "tasks.md",
		"spec:change-edits": "specs/change-edits/spec.md",
	} {
		f, err := ParseFile(in)
		if err != nil || f.Rel() != rel || f.String() != in {
			t.Errorf("%s: %v %v", in, f, err)
		}
	}
	for _, bad := range []string{"readme", "spec:", "spec:../x", "spec"} {
		if _, err := ParseFile(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

// Real openspec validate --strict --json output (openspec 1.16).
const (
	realValid = `{"items":[{"id":"add-login","type":"change","valid":true,"issues":[],"durationMs":16}],"summary":{"totals":{"items":1,"passed":1,"failed":0}},"version":"1.0"}`
	realBad   = `{"items":[{"id":"add-login","type":"change","valid":false,"issues":[{"level":"ERROR","path":"change-edits/spec.md","message":"ADDED \"Validating a change\" must include at least one scenario"}],"durationMs":20}],"version":"1.0"}`
	realGone  = `{"status":[{"severity":"error","code":"unknown_item","message":"Unknown item 'nope'."}]}`
)

func TestParseValidate(t *testing.T) {
	r := parseValidate("add-login", []byte(realValid), nil, true)
	if !r.Valid || len(r.Errors) != 0 || r.Version != ValidateVersion {
		t.Errorf("valid: %+v", r)
	}
	r = parseValidate("add-login", []byte(realBad), nil, false)
	if r.Valid || len(r.Errors) != 1 || r.Errors[0].Path != "change-edits/spec.md" || !strings.Contains(r.Text(), `"Validating a change" must include at least one scenario`) {
		t.Errorf("invalid: %+v\n%s", r, r.Text())
	}
	r = parseValidate("nope", []byte(realGone), nil, false)
	if r.Valid || len(r.Errors) != 1 || r.Errors[0].Level != "ERROR" || !strings.Contains(r.Errors[0].Message, "Unknown item") {
		t.Errorf("unknown: %+v", r)
	}
	// Not JSON: judged by the exit status.
	r = parseValidate("x", []byte("Change 'x' has issues\nbad thing\n"), nil, false)
	if r.Valid || len(r.Errors) != 1 || !strings.Contains(r.Errors[0].Message, "bad thing") {
		t.Errorf("text: %+v", r)
	}
	if r := parseValidate("x", []byte("Change 'x' is valid\n"), nil, true); !r.Valid || r.Errors == nil {
		t.Errorf("text valid: %+v", r)
	}
}

// Scenario: Edit breaks a spec, against the real openspec when installed.
func TestValidateRealOpenSpec(t *testing.T) {
	if _, err := exec.LookPath("openspec"); err != nil {
		t.Skip("openspec not installed")
	}
	repo := newRepo(t)
	dir := filepath.Join(repo, "openspec", "changes", "add-login")
	spec := "## ADDED Requirements\n\n### Requirement: Login\nThe system SHALL let a user log in.\n\n#### Scenario: Log in\n- **WHEN** a user logs in\n- **THEN** they are in\n"
	testenv.WriteFile(t, filepath.Join(dir, "specs", "auth", "spec.md"), spec)
	testenv.WriteFile(t, filepath.Join(dir, "proposal.md"), "# Add login\n\n## Why\n\nUsers need to log in to see their own data in the app.\n\n## What Changes\n\n- Add login.\n")
	if err := os.RemoveAll(filepath.Join(dir, "specs", "users")); err != nil {
		t.Fatal(err)
	}
	p := Place{Change: "add-login", Dir: repo}
	res, err := Validator{}.Validate(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("valid change rejected: %s", res.Text())
	}
	// Remove the only scenario.
	testenv.WriteFile(t, filepath.Join(dir, "specs", "auth", "spec.md"), strings.Split(spec, "#### Scenario")[0])
	res, err = Validator{}.Validate(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid || !strings.Contains(res.Text(), "Login") || !strings.Contains(res.Text(), "scenario") {
		t.Errorf("broken spec: %s", res.Text())
	}
}

// newRepo makes a git repository with change add-login committed.
func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	testenv.Git(t, repo, "init", "-q", "-b", "main")
	testenv.Git(t, repo, "config", "user.name", "Test")
	testenv.Git(t, repo, "config", "user.email", "test@example.invalid")
	testenv.Git(t, repo, "config", "commit.gpgsign", "false")
	testenv.AddChange(t, repo, "add-login")
	testenv.WriteFile(t, filepath.Join(repo, "openspec", "changes", "add-login", "specs", "users", "spec.md"), "x\n")
	testenv.WriteFile(t, filepath.Join(repo, "openspec", "changes", "add-login", "specs", "auth", "spec.md"), "x\n")
	testenv.Git(t, repo, "add", "-A")
	testenv.Git(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// addWorktree makes a worktree for change with run state in it, as a run
// leaves it.
func addWorktree(t *testing.T, repo, change string, state runs.State) (string, statedir.Dir) {
	t.Helper()
	wt := filepath.Join(filepath.Dir(repo), "repo-worktrees", change)
	testenv.Git(t, repo, "worktree", "add", "-q", "-b", change, wt)
	if r, err := filepath.EvalSymlinks(wt); err == nil {
		wt = r
	}
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: change, Worktree: wt, State: state, Step: "build", Round: 2}); err != nil {
		t.Fatal(err)
	}
	return wt, st
}

// Scenarios: Change with a worktree; Change without a worktree.
func TestLocate(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	p, err := Locate(ctx, repo, "add-login")
	if err != nil || p.Dir != repo || p.Worktree || p.State != "" {
		t.Fatalf("without a worktree: %+v %v", p, err)
	}
	if !strings.Contains(p.Describe(), "repository checkout") {
		t.Errorf("describe: %s", p.Describe())
	}
	wt, st := addWorktree(t, repo, "add-login", runs.Stopped)
	// From the checkout and from inside the worktree alike.
	for _, from := range []string{repo, filepath.Join(wt, "openspec")} {
		p, err = Locate(ctx, from, "add-login")
		if err != nil || p.Dir != wt || !p.Worktree || p.State != st {
			t.Fatalf("from %s: %+v %v", from, p, err)
		}
	}
	if _, err := Locate(ctx, repo, "no-such"); err == nil || !strings.Contains(err.Error(), "no change no-such") {
		t.Errorf("unknown change: %v", err)
	}
	if _, err := Locate(ctx, repo, "../x"); err == nil {
		t.Error("bad id accepted")
	}
}

// Scenario: Unknown spec.
func TestUnknownSpecListsDeltas(t *testing.T) {
	repo := newRepo(t)
	p := Place{Change: "add-login", Dir: repo}
	f, _ := ParseFile("spec:billing")
	_, err := f.Path(p)
	if err == nil || !strings.Contains(err.Error(), "no billing spec delta") || !strings.Contains(err.Error(), "auth, users") {
		t.Errorf("error %v", err)
	}
	f, _ = ParseFile("spec:auth")
	if path, err := f.Path(p); err != nil || !strings.HasSuffix(filepath.ToSlash(path), "add-login/specs/auth/spec.md") {
		t.Errorf("auth: %s %v", path, err)
	}
}

// No edits during a run: the lock is held by a run (another holder without
// an edit marker), by another edit, or not at all.
func TestWithChangeLock(t *testing.T) {
	repo := newRepo(t)
	wt, st := addWorktree(t, repo, "add-login", runs.Running)
	ctx := context.Background()
	p, err := Locate(ctx, repo, "add-login")
	if err != nil {
		t.Fatal(err)
	}

	// A run holds the lock (here, this test holds it as a run would).
	runLock, err := runs.TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	err = WithChangeLock(ctx, p, "tuckerbox tasks tick 1.1", func() error { called = true; return nil })
	var ar *ActiveRunError
	if !errors.As(err, &ar) || called || !errors.Is(err, runs.ErrLocked) {
		t.Fatalf("with a run: %v (called %v)", err, called)
	}
	if !strings.Contains(err.Error(), "running step build (round 2)") || !strings.Contains(err.Error(), wt) {
		t.Errorf("message: %v", err)
	}
	runLock.Release()

	// Free: fn runs holding the lock, with the edit marker.
	err = WithChangeLock(ctx, p, "tuckerbox edit design", func() error {
		called = true
		if !runs.Active(st.RunLock()) {
			t.Error("lock not held")
		}
		m, ok := runs.ReadEditMarker(st)
		if !ok || m.Command != "tuckerbox edit design" || m.Pid != os.Getpid() {
			t.Errorf("marker %+v %v", m, ok)
		}
		// A second edit is refused as being edited.
		inner := WithChangeLock(ctx, p, "tuckerbox approve", func() error { t.Error("nested edit ran"); return nil })
		var ee *runs.EditingError
		if !errors.As(inner, &ee) || !strings.Contains(inner.Error(), "being edited (tuckerbox edit design") {
			t.Errorf("second edit: %v", inner)
		}
		return errors.New("from fn")
	})
	if !called || err == nil || err.Error() != "from fn" {
		t.Errorf("free lock: %v %v", called, err)
	}
	if runs.Active(st.RunLock()) {
		t.Error("lock still held")
	}
	if _, ok := runs.ReadEditMarker(st); ok {
		t.Error("marker left behind")
	}

	// The checkout has no lock to take.
	called = false
	if err := WithChangeLock(ctx, Place{Change: "add-login", Dir: repo}, "x", func() error { called = true; return nil }); err != nil || !called {
		t.Errorf("checkout: %v %v", called, err)
	}
}

// A queued run is named as queued.
func TestActiveRunErrorQueued(t *testing.T) {
	e := &ActiveRunError{Change: "c", Worktree: "/w", State: runs.Queued, Step: "plan", Round: 1, Pid: 7}
	if s := e.Error(); !strings.Contains(s, "queued for step plan (round 1)") || !strings.Contains(s, "pid 7") {
		t.Error(s)
	}
}

// A run that takes the lock clears a marker an edit that died left.
func TestEditMarkerStale(t *testing.T) {
	repo := newRepo(t)
	_, st := addWorktree(t, repo, "add-login", runs.Stopped)
	if err := runs.WriteEditMarker(st, "tuckerbox edit design"); err != nil {
		t.Fatal(err)
	}
	l, err := runs.TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	// LockedError trusts a marker; the engine removes it on taking the lock
	// (see engine.go), which TestEditBlocksRun in internal/cli exercises.
	var ee *runs.EditingError
	if err := runs.LockedError("add-login", "/w", st); !errors.As(err, &ee) {
		t.Errorf("%v", err)
	}
	runs.RemoveEditMarker(st)
	var ae *runs.ActiveError
	if err := runs.LockedError("add-login", "/w", st); !errors.As(err, &ae) {
		t.Errorf("%v", err)
	}
}
