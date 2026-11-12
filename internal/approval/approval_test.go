package approval

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copyFixture copies testdata/change to a scratch folder, converting line
// endings to CRLF when crlf is set, and returns it.
func copyFixture(t *testing.T, crlf bool) Dir {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("testdata/change", func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/change", p)
		out := filepath.Join(dst, rel)
		if e.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// A Windows checkout may have converted the fixture already.
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
		if crlf {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}
		return os.WriteFile(out, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return Dir(dst)
}

func expectedHash(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/hash.txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func mustHash(t *testing.T, f Files) string {
	t.Helper()
	h, err := Hash(f)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func write(t *testing.T, d Dir, rel, data string) {
	t.Helper()
	p := filepath.Join(string(d), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, d Dir, rel string) string {
	t.Helper()
	data, err := d.Read(rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The fixture's hash is fixed in testdata/hash.txt, so macOS, Linux and
// Windows CI all check the same value.
func TestHashFixture(t *testing.T) {
	if got, want := mustHash(t, copyFixture(t, false)), expectedHash(t); got != want {
		t.Errorf("hash %s, want %s (testdata/hash.txt)", got, want)
	}
}

// Scenario: Line endings.
func TestHashCRLFEqualsLF(t *testing.T) {
	if got, want := mustHash(t, copyFixture(t, true)), expectedHash(t); got != want {
		t.Errorf("CRLF hash %s, want the LF hash %s", got, want)
	}
}

func TestHashWhatCounts(t *testing.T) {
	base := mustHash(t, copyFixture(t, false))
	for _, tc := range []struct {
		name   string
		change func(d Dir)
		same   bool
	}{
		{"tick a task", func(d Dir) {
			write(t, d, "tasks.md", strings.Replace(read(t, d, "tasks.md"), "- [ ] 1.1", "- [x] 1.1", 1))
		}, true},
		{"untick a task", func(d Dir) {
			write(t, d, "tasks.md", strings.Replace(read(t, d, "tasks.md"), "- [x] 1.2", "- [ ] 1.2", 1))
		}, true},
		{"edit tuckerbox.toml", func(d Dir) { write(t, d, SidecarName, "depends_on = []\ntitle = \"x\"\n") }, true},
		{"add a file outside the hash", func(d Dir) { write(t, d, "notes.txt", "x") }, true},
		{"reword a task", func(d Dir) {
			write(t, d, "tasks.md", strings.Replace(read(t, d, "tasks.md"), "Add the form", "Add a form", 1))
		}, false},
		{"edit design", func(d Dir) { write(t, d, "design.md", read(t, d, "design.md")+"More.\n") }, false},
		{"edit a spec", func(d Dir) { write(t, d, "specs/users/spec.md", "changed\n") }, false},
		{"add a spec", func(d Dir) { write(t, d, "specs/billing/spec.md", "x\n") }, false},
		{"empty model.md instead of none", func(d Dir) { write(t, d, "model.md", "") }, false},
		{"move text between files", func(d Dir) {
			write(t, d, "design.md", read(t, d, "design.md")+read(t, d, "proposal.md"))
			write(t, d, "proposal.md", "")
		}, false},
		{"bare checkbox not a task", func(d Dir) {
			write(t, d, "design.md", strings.Replace(read(t, d, "design.md"), "Sessions", "[x] Sessions", 1))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := copyFixture(t, false)
			tc.change(d)
			if got := mustHash(t, d); (got == base) != tc.same {
				t.Errorf("hash changed = %v, want %v", got != base, !tc.same)
			}
		})
	}
}

func TestHashRemovedFileDiffersFromEmpty(t *testing.T) {
	d := copyFixture(t, false)
	if err := os.Remove(filepath.Join(string(d), "design.md")); err != nil {
		t.Fatal(err)
	}
	missing := mustHash(t, d)
	write(t, d, "design.md", "")
	if mustHash(t, d) == missing {
		t.Error("an empty design.md hashes like a missing one")
	}
}

var at = time.Date(2026, 11, 12, 9, 0, 0, 0, time.FixedZone("AEST", 10*3600))

func judge(t *testing.T, f Files) State {
	t.Helper()
	st, err := Judge(f)
	if err != nil {
		t.Fatal(err)
	}
	return st.State
}

// Scenario: Approve a valid change (the file part; the command is tested in
// internal/cli).
func TestApproveKeepsOtherKeysAndComments(t *testing.T) {
	d := copyFixture(t, false)
	before := "# Dependencies for add-login\ndepends_on = [\"add-users\"] # keep\n\n[extra]\nkey = 1 # comment\n"
	write(t, d, SidecarName, before)
	if judge(t, d) != None {
		t.Fatal("state before approve is not none")
	}
	a, err := Approve(d, `Sam "Q" Example`, at)
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, d, SidecarName)
	if !strings.HasPrefix(got, before) {
		t.Errorf("other lines changed:\n%s", got)
	}
	want := "\n[approval]\nhash = \"" + a.Hash + "\"\nsettings_hash = \"" + SettingsHash(nil) + "\"\nby = \"Sam \\\"Q\\\" Example\"\nat = 2026-11-12T09:00:00+10:00\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("approval table:\n%s\nwant suffix:\n%s", got, want)
	}
	st, err := Judge(d)
	if err != nil || st.State != Approved || st.Approval.By != `Sam "Q" Example` || !st.Approval.At.Equal(at) {
		t.Fatalf("after approve: %+v %v", st, err)
	}

	// Approving again replaces the table in place.
	write(t, d, "design.md", "changed\n")
	if judge(t, d) != Stale {
		t.Fatal("not stale after an edit")
	}
	if _, err := Approve(d, "Other", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	again := read(t, d, SidecarName)
	if strings.Count(again, "[approval]") != 1 || !strings.HasPrefix(again, before) || !strings.Contains(again, `by = "Other"`) {
		t.Errorf("re-approve:\n%s", again)
	}
	if judge(t, d) != Approved {
		t.Error("not approved after re-approve")
	}

	// Scenario: Unapprove (file part).
	removed, err := Unapprove(d)
	if err != nil || !removed {
		t.Fatalf("unapprove: %v %v", removed, err)
	}
	if got := read(t, d, SidecarName); got != before {
		t.Errorf("after unapprove:\n%q\nwant:\n%q", got, before)
	}
	if judge(t, d) != None {
		t.Error("not none after unapprove")
	}
	removed, err = Unapprove(d)
	if err != nil || removed {
		t.Errorf("second unapprove: %v %v", removed, err)
	}
	if got := read(t, d, SidecarName); got != before {
		t.Errorf("second unapprove changed the file:\n%s", got)
	}
}

func TestApproveTableInTheMiddle(t *testing.T) {
	in := "depends_on = [\"a\"]\r\n\r\n[approval]\r\nhash = \"sha256:old\"\r\nby = \"X\"\r\nat = 2026-01-01T00:00:00Z\r\n\r\n# about extra\r\n[extra]\r\nk = 1\r\n"
	out, err := SetApproval([]byte(in), Approval{Hash: "sha256:new", By: "Y", At: at})
	if err != nil {
		t.Fatal(err)
	}
	want := "depends_on = [\"a\"]\r\n\r\n[approval]\r\nhash = \"sha256:new\"\r\nby = \"Y\"\r\nat = 2026-11-12T09:00:00+10:00\r\n\r\n# about extra\r\n[extra]\r\nk = 1\r\n"
	if string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
	removed, ok, err := RemoveApproval(out)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if want := "depends_on = [\"a\"]\r\n\r\n\r\n# about extra\r\n[extra]\r\nk = 1\r\n"; string(removed) != want {
		t.Errorf("removed:\n%q\nwant:\n%q", removed, want)
	}
}

func TestApproveCreatesAndUnapproveRemovesSidecar(t *testing.T) {
	d := copyFixture(t, false)
	if err := os.Remove(d.SidecarPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(d, "Sam", at); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(t, d, SidecarName), "[approval]\n") {
		t.Errorf("new sidecar:\n%s", read(t, d, SidecarName))
	}
	if _, err := Unapprove(d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.SidecarPath()); !os.IsNotExist(err) {
		t.Errorf("empty sidecar left behind: %v", err)
	}
}

func TestRefusesLayoutsItCannotRewrite(t *testing.T) {
	for _, in := range []string{
		"approval = { hash = \"sha256:x\" }\n",
		"approval.hash = \"sha256:x\"\n",
		"not toml = = \n",
	} {
		if _, err := SetApproval([]byte(in), Approval{Hash: "sha256:y", By: "Y", At: at}); err == nil {
			t.Errorf("SetApproval accepted %q", in)
		}
		if _, _, err := RemoveApproval([]byte(in)); err == nil {
			t.Errorf("RemoveApproval accepted %q", in)
		}
	}
}

// Scenario: Edit pulled on another machine. Two clones of one repository
// stand for two machines: both judge the same pulled files the same way.
func TestStaleOnEveryMachine(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	gitT := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin := filepath.Join(t.TempDir(), "origin")
	gitT(".", "init", "-q", "--bare", "-b", "main", origin)
	one, two := filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")
	gitT(".", "clone", "-q", origin, one)
	src := copyFixture(t, false)
	d1 := DirOf(one, "add-login")
	if err := os.CopyFS(string(d1), os.DirFS(string(src))); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(d1, "Sam", at); err != nil {
		t.Fatal(err)
	}
	gitT(one, "add", "-A")
	gitT(one, "commit", "-q", "-m", "approve add-login")
	gitT(one, "push", "-q", "origin", "HEAD:main")
	gitT(".", "clone", "-q", origin, two)
	d2 := DirOf(two, "add-login")
	if judge(t, d1) != Approved || judge(t, d2) != Approved {
		t.Fatal("not approved on both machines after push")
	}

	write(t, d2, "design.md", "Sessions are tokens.\n")
	gitT(two, "commit", "-q", "-am", "edit design")
	gitT(two, "push", "-q", "origin", "HEAD:main")
	gitT(one, "pull", "-q", "origin", "main")
	if a, b := judge(t, d1), judge(t, d2); a != Stale || b != Stale {
		t.Errorf("machine one %s, machine two %s; want stale on both", a, b)
	}

	// The same judgement from the committed revision, as the run gate
	// reads the base branch.
	rev, err := NewRev(context.Background(), one, "origin/main", "add-login")
	if err != nil {
		t.Fatal(err)
	}
	if judge(t, rev) != Stale {
		t.Error("revision not stale")
	}
	if mustHash(t, rev) != mustHash(t, d1) {
		t.Error("revision and checkout hash differently")
	}
	missing, err := NewRev(context.Background(), one, "origin/main", "no-such-change")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Exists() || judge(t, missing) != None {
		t.Error("missing change has files or an approval")
	}
}
