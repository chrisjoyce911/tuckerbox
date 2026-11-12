package statedir

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func same(t *testing.T, a, b string) bool {
	t.Helper()
	return strings.EqualFold(resolved(a), resolved(b))
}

// resolved resolves symlinks in the longest existing prefix of p (macOS /tmp is a
// symlink; Windows may report short names).
func resolved(p string) string {
	p = filepath.Clean(p)
	if e, err := filepath.EvalSymlinks(p); err == nil {
		return e
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(resolved(parent), filepath.Base(p))
}

func TestResolveMainAndWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")

	d, err := Resolve(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !same(t, filepath.Dir(string(d)), filepath.Join(repo, ".git")) {
		t.Errorf("main state dir = %s, want under %s", d, filepath.Join(repo, ".git"))
	}

	wt := filepath.Join(t.TempDir(), "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "fix-login", wt)
	wd, err := Resolve(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if same(t, string(wd), string(d)) {
		t.Errorf("worktree shares the main state dir %s", d)
	}
	if !filepath.IsAbs(string(wd)) {
		t.Errorf("state dir %s is not absolute", wd)
	}
	c1, err := ResolveCommon(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if !same(t, c1, string(d)) {
		t.Errorf("common dir %s, want %s", c1, d)
	}

	if err := wd.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Dir(wd.Brief("build", 1)), filepath.Dir(wd.Result("build", 1)), filepath.Dir(wd.Log("build"))} {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Errorf("%s not created: %v", p, err)
		}
	}
	if wd.Result("build", 1) == wd.Result("build", 2) {
		t.Error("result paths must differ per round")
	}
}

func TestResolveOutsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	if _, err := Resolve(context.Background(), dir); err == nil {
		t.Fatal("want an error outside a repository")
	}
}

func TestCheckID(t *testing.T) {
	for _, ok := range []string{"build", "review-2", "plan_a", "v1.2"} {
		if err := CheckID("step", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", `a\b`, "..", "-x", "a b"} {
		if err := CheckID("step", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
