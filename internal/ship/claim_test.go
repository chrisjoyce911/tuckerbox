package ship

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}
func writeTest(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

type repoFixture struct{ bare, one, two string }

func repositories(t *testing.T) repoFixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	root := t.TempDir()
	f := repoFixture{filepath.Join(root, "remote.git"), filepath.Join(root, "one"), filepath.Join(root, "two")}
	gitTest(t, root, "init", "--bare", "--initial-branch=main", f.bare)
	for i, dir := range []string{f.one, f.two} {
		gitTest(t, root, "clone", f.bare, dir)
		gitTest(t, dir, "config", "user.name", "Test")
		gitTest(t, dir, "config", "user.email", "test@example.org")
		if i == 0 {
			writeTest(t, dir, "base.txt", "base\n")
			gitTest(t, dir, "add", ".")
			gitTest(t, dir, "commit", "-m", "base")
			gitTest(t, dir, "push", "origin", "main")
		} else {
			gitTest(t, dir, "fetch", "origin")
			gitTest(t, dir, "checkout", "main")
		}
		gitTest(t, dir, "checkout", "-b", "add-login")
		writeTest(t, dir, "fix.txt", fmt.Sprintf("machine %d\n", i))
		gitTest(t, dir, "add", ".")
		gitTest(t, dir, "commit", "-m", "change")
	}
	return f
}
func (f repoFixture) claim(dir, machine string) *Claim {
	return &Claim{Identity: Identity{Change: "add-login", Machine: machine, Run: "run1"}, Git: Git{Worktree: dir, Remote: "origin", Branch: "add-login", Base: "main", DefaultBranch: "main"}}
}

type fakeForge struct {
	mu        sync.Mutex
	mr        *forge.MR
	opens     int
	openError error
}

func (f *fakeForge) FindMR(context.Context, string) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mr == nil {
		return nil, nil
	}
	m := *f.mr
	return &m, nil
}
func (f *fakeForge) OpenMR(_ context.Context, r forge.OpenRequest) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	if f.openError != nil {
		return nil, f.openError
	}
	if f.mr != nil {
		return nil, &forge.APIError{Status: 422, Message: "already exists"}
	}
	f.mr = &forge.MR{Number: 1, State: "open", URL: "https://forge.example/mr/1", Body: r.Body}
	m := *f.mr
	return &m, nil
}
func savedTo(p *string) SaveLease { return func(s string) error { *p = s; return nil } }
func TestMarker(t *testing.T) {
	want := Identity{Change: "add-login", Machine: "studio", Run: "run1"}
	marker, err := want.Marker()
	if err != nil || marker != "<!-- tuckerbox: change=add-login machine=studio run=run1 -->" {
		t.Fatal(marker, err)
	}
	got, ok := ParseMarker("visible body\n\n" + marker)
	if !ok || got != want {
		t.Fatal(got, ok)
	}
	for _, body := range []string{marker + marker, "<!-- tuckerbox: change=x machine=y -->", "<!-- tuckerbox: change=x machine=y run=z extra=x -->", "<!-- tuckerbox: change=x machine=y machine=z -->"} {
		if _, ok := ParseMarker(body); ok {
			t.Fatal(body)
		}
	}
	for _, name := range []string{"", "has space", "evil-->", "x=y"} {
		if _, err := (Identity{Change: "change", Machine: name, Run: "run"}).Marker(); err == nil {
			t.Fatal(name)
		}
	}
	if got, err := MachineName("studio"); err != nil || got != "studio" {
		t.Fatal(got, err)
	}
	if got, err := MachineName(""); err != nil || got == "" {
		t.Fatal(got, err)
	}
}
func TestFirstPushAndPersistBeforeOpen(t *testing.T) {
	f := repositories(t)
	c := f.claim(f.one, "studio")
	api := &fakeForge{}
	saved := ""
	mr, err := c.Acquire(context.Background(), api, forge.OpenRequest{Title: "add login", Body: "proposal"}, savedTo(&saved))
	if err != nil || saved == "" || saved != c.Lease || mr == nil {
		t.Fatal(mr, saved, err)
	}
	if got := gitTest(t, f.one, "ls-remote", "origin", "refs/heads/add-login"); !strings.HasPrefix(got, saved) {
		t.Fatal(got)
	}
	if err := c.Check(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	// A new fix uses the recorded lease; even updating a tracking ref cannot bypass it.
	writeTest(t, f.one, "fix.txt", "fixed\n")
	gitTest(t, f.one, "add", ".")
	gitTest(t, f.one, "commit", "-m", "fix")
	if _, err := c.Acquire(context.Background(), api, forge.OpenRequest{}, savedTo(&saved)); err != nil || api.opens != 1 {
		t.Fatal(api.opens, err)
	}
}
func TestOnlyOneMachineClaims(t *testing.T) {
	f := repositories(t)
	api := &fakeForge{}
	a, b := f.claim(f.one, "studio"), f.claim(f.two, "mini")
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, c := range []*Claim{a, b} {
		wg.Add(1)
		go func(c *Claim) {
			defer wg.Done()
			_, err := c.Acquire(context.Background(), api, forge.OpenRequest{}, func(string) error { return nil })
			errors <- err
		}(c)
	}
	wg.Wait()
	close(errors)
	winners := 0
	for err := range errors {
		if err == nil {
			winners++
		} else if !strings.Contains(err.Error(), "claimed by") && !strings.Contains(err.Error(), "remote branch exists") && !strings.Contains(err.Error(), "branch changed") {
			t.Fatal(err)
		}
	}
	if winners != 1 || api.opens != 1 {
		t.Fatalf("winners=%d opens=%d", winners, api.opens)
	}
	owner, _ := ParseMarker(api.mr.Body)
	winner := a
	if owner.Machine == "mini" {
		winner = b
	}
	tip, err := winner.Git.Tip(context.Background())
	if err != nil || tip != winner.Lease {
		t.Fatal(tip, err)
	}
}
func TestForeignMRAndLeftoverBranch(t *testing.T) {
	f := repositories(t)
	ctx := context.Background()
	c := f.claim(f.one, "studio")
	for _, body := range []string{"person's MR", "<!-- tuckerbox: change=add-login machine=mini run=run1 -->"} {
		api := &fakeForge{mr: &forge.MR{Body: body, URL: "https://forge.example/mr/1"}}
		if _, err := c.Acquire(ctx, api, forge.OpenRequest{}, func(string) error { return nil }); err == nil || !strings.Contains(err.Error(), "claimed by") {
			t.Fatal(err)
		}
		tip, _ := c.Git.Tip(ctx)
		if tip != "" {
			t.Fatal("foreign claim was pushed")
		}
	}
	gitTest(t, f.two, "push", "origin", "add-login")
	before := gitTest(t, f.two, "rev-parse", "HEAD")
	if _, err := c.Acquire(ctx, &fakeForge{}, forge.OpenRequest{}, func(string) error { return nil }); err == nil || err.Error() != "remote branch exists" {
		t.Fatal(err)
	}
	tip, _ := c.Git.Tip(ctx)
	if tip != before {
		t.Fatal(tip)
	}
}
func humanFix(t *testing.T, f repoFixture) string {
	t.Helper()
	gitTest(t, f.two, "fetch", "origin")
	gitTest(t, f.two, "reset", "--hard", "origin/add-login")
	writeTest(t, f.two, "human.txt", "person's fix\n")
	gitTest(t, f.two, "add", ".")
	gitTest(t, f.two, "commit", "-m", "person's fix")
	gitTest(t, f.two, "push", "origin", "add-login")
	return gitTest(t, f.two, "rev-parse", "HEAD")
}
func TestMovedHeadBlocksAndResumeAdopts(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		t.Run(fmt.Sprint(dirty), func(t *testing.T) {
			f := repositories(t)
			ctx := context.Background()
			c := f.claim(f.one, "studio")
			api := &fakeForge{}
			saved := ""
			if _, err := c.Acquire(ctx, api, forge.OpenRequest{}, savedTo(&saved)); err != nil {
				t.Fatal(err)
			}
			old := c.Lease
			tip := humanFix(t, f)
			if err := c.Check(ctx, api); err == nil || err.Error() != "branch changed on the forge" {
				t.Fatal(err)
			}
			if _, err := c.Git.Push(ctx, old); err == nil {
				t.Fatal("stale lease pushed")
			}
			if dirty {
				writeTest(t, f.one, "local.txt", "keep me\n")
			}
			_, err := c.Resume(ctx, api, forge.OpenRequest{}, savedTo(&saved))
			if dirty {
				if err == nil || !strings.Contains(err.Error(), "not clean") {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(filepath.Join(f.one, "local.txt"))
				if string(data) != "keep me\n" {
					t.Fatal("local work changed")
				}
				if c.Lease != old {
					t.Fatal("failed adoption changed lease")
				}
			} else {
				if err != nil || c.Lease != tip || saved != tip || gitTest(t, f.one, "rev-parse", "HEAD") != tip || api.opens != 1 {
					t.Fatal(c, saved, err)
				}
			}
		})
	}
}
func TestCrashBetweenPushAndOpen(t *testing.T) {
	f := repositories(t)
	ctx := context.Background()
	c := f.claim(f.one, "studio")
	api := &fakeForge{openError: fmt.Errorf("network down")}
	saved := ""
	if _, err := c.Acquire(ctx, api, forge.OpenRequest{}, savedTo(&saved)); err == nil || saved == "" {
		t.Fatal(saved, err)
	}
	restarted := f.claim(f.one, "studio")
	restarted.Lease = saved
	api.openError = nil
	if mr, err := restarted.Resume(ctx, api, forge.OpenRequest{}, savedTo(&saved)); err != nil || mr == nil || restarted.Lease != c.Lease {
		t.Fatal(mr, err)
	}
}
func TestNeverPushBaseOrDefault(t *testing.T) {
	f := repositories(t)
	gitTest(t, f.one, "checkout", "main")
	for _, g := range []Git{{Worktree: f.one, Branch: "main", Base: "origin/main"}, {Worktree: f.one, Branch: "main", Base: "other", DefaultBranch: "main"}, {Worktree: f.one, Branch: "main", Base: "other"}} {
		if _, err := g.Push(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "base branch") && !strings.Contains(err.Error(), "default branch") {
			t.Fatal(err)
		}
	}
}

type openRace struct {
	*fakeForge
	first bool
}

func (f *openRace) FindMR(ctx context.Context, branch string) (*forge.MR, error) {
	if f.first {
		f.first = false
		return nil, nil
	}
	return f.fakeForge.FindMR(ctx, branch)
}
func TestAlreadyOpenRaceChecksMarker(t *testing.T) {
	for _, machine := range []string{"studio", "mini"} {
		t.Run(machine, func(t *testing.T) {
			f := repositories(t)
			c := f.claim(f.one, "studio")
			marker, err := (Identity{Change: "add-login", Machine: machine, Run: "run1"}).Marker()
			if err != nil {
				t.Fatal(err)
			}
			api := &openRace{&fakeForge{mr: &forge.MR{Number: 1, State: "open", Body: marker, URL: "https://forge.example/mr/1"}, openError: &forge.APIError{Status: 422, Message: "already open"}}, true}
			_, err = c.Acquire(context.Background(), api, forge.OpenRequest{}, func(string) error { return nil })
			if machine == "studio" && err != nil {
				t.Fatal(err)
			}
			if machine == "mini" && (err == nil || !strings.Contains(err.Error(), "claimed by mini")) {
				t.Fatal(err)
			}
		})
	}
}
func TestLeaseMustBeSavedBeforeOpening(t *testing.T) {
	f := repositories(t)
	c := f.claim(f.one, "studio")
	api := &fakeForge{}
	_, err := c.Acquire(context.Background(), api, forge.OpenRequest{}, func(string) error { return fmt.Errorf("disk full") })
	if err == nil || api.opens != 0 {
		t.Fatal(api.opens, err)
	}
	// The remote push happened; preserving that ref is safer than attempting rollback.
	tip, err := c.Git.Tip(context.Background())
	if err != nil || tip == "" {
		t.Fatal(tip, err)
	}
}
