package fleet

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func stores(t *testing.T) (Store, Store, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	gitTest(t, root, "init", "--bare", "--initial-branch=main", bare)
	dirs := []string{filepath.Join(root, "one"), filepath.Join(root, "two")}
	for i, d := range dirs {
		gitTest(t, root, "clone", bare, d)
		gitTest(t, d, "config", "user.name", "Test")
		gitTest(t, d, "config", "user.email", "test@example.org")
		if i == 0 {
			gitTest(t, d, "commit", "--allow-empty", "-m", "base")
			gitTest(t, d, "push", "origin", "main")
		} else {
			gitTest(t, d, "fetch", "origin")
			gitTest(t, d, "checkout", "main")
		}
	}
	return Store{Dir: dirs[0], Lease: 10 * time.Minute}, Store{Dir: dirs[1], Lease: 10 * time.Minute}, bare
}
func claim(t *testing.T, s Store, change, machine string) Claim {
	t.Helper()
	a, err := s.Acquire(ctx, change, machine, "run", change, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return a.Claim
}
func wantErr(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("want %q, got %v", text, err)
	}
}
func TestTwoMachinesClaimSameChange(t *testing.T) {
	a, b, _ := stores(t)
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	var onceA, onceB sync.Once
	a.beforePush = func() { onceA.Do(func() { wg.Done(); <-barrier }) }
	b.beforePush = func() { onceB.Do(func() { wg.Done(); <-barrier }) }
	errs := make(chan error, 2)
	for i, s := range []Store{a, b} {
		go func(i int, s Store) {
			_, err := s.Acquire(ctx, "login", []string{"one", "two"}[i], "run", "login", false, false)
			errs <- err
		}(i, s)
	}
	wg.Wait()
	close(barrier)
	x, y := <-errs, <-errs
	if (x == nil) == (y == nil) {
		t.Fatalf("exactly one winner: %v / %v", x, y)
	}
	if x != nil {
		wantErr(t, x, "claimed by")
	} else {
		wantErr(t, y, "claimed by")
	}
}
func TestConcurrentDifferentChangesRetryWithoutLoss(t *testing.T) {
	a, b, _ := stores(t)
	var once sync.Once
	a.beforePush = func() { once.Do(func() { claim(t, b, "users", "two") }) }
	claim(t, a, "login", "one")
	all, err := a.Read(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("lost write: %v %v", all, err)
	}
}
func TestLimitReadyAndNextScope(t *testing.T) {
	a, b, _ := stores(t)
	a.MaxRuns = 1
	b.MaxRuns = 1
	c := claim(t, a, "login", "one")
	_, err := b.Acquire(ctx, "users", "two", "run", "users", false, false)
	wantErr(t, err, "fleet limit reached: login")
	if err = a.SetState(ctx, c, Ready); err != nil {
		t.Fatal(err)
	}
	d := claim(t, b, "users", "two")
	wantErr(t, a.SetState(ctx, c, Active), "fleet limit reached: users")
	all, _ := a.Read(ctx)
	if all["login"].State != Ready {
		t.Fatal("refused admission changed ready claim")
	}
	if err = b.SetState(ctx, d, Ready); err != nil {
		t.Fatal(err)
	}
	if err = a.SetState(ctx, c, Active); err != nil {
		t.Fatal(err)
	}
	all, _ = a.Read(ctx)
	if all["login"].Epoch != c.Epoch || all["login"].State != Active {
		t.Fatal(all)
	}
}
func TestParkedNeverExpiresAndRequiresForce(t *testing.T) {
	a, b, _ := stores(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	c := claim(t, a, "login", "one")
	if err := a.SetState(ctx, c, Parked); err != nil {
		t.Fatal(err)
	}
	b.Now = func() time.Time { return now.Add(24 * time.Hour) }
	_, err := b.Acquire(ctx, "login", "two", "new", "login", true, false)
	wantErr(t, err, "--force")
	next, err := b.Acquire(ctx, "login", "two", "new", "login", true, true)
	if err != nil || next.Claim.Epoch != 2 {
		t.Fatalf("%+v %v", next, err)
	}
	wantErr(t, a.Renew(ctx, c), "claim taken by two")
}
func TestExpiryIsDerivedWithSkewAndNotRemoved(t *testing.T) {
	a, b, _ := stores(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.Now = func() time.Time { return now }
	c := claim(t, a, "login", "one")
	if c.Expired(now.Add(11*time.Minute), a.Lease) {
		t.Fatal("skew boundary expired")
	}
	if !c.Expired(now.Add(11*time.Minute+time.Second), a.Lease) {
		t.Fatal("not expired")
	}
	b.Now = func() time.Time { return now.Add(12 * time.Minute) }
	all, _ := b.Read(ctx)
	if len(all) != 1 {
		t.Fatal("expiry removed claim")
	}
	_, err := b.Acquire(ctx, "login", "two", "new", "login", false, false)
	wantErr(t, err, "--take-over")
	next, err := b.Acquire(ctx, "login", "two", "new", "login", true, false)
	if err != nil || next.BranchTip != "" || next.Claim.Epoch != 2 {
		t.Fatalf("%+v %v", next, err)
	}
}
func TestTakeoverFencesPushAndPinnedMerge(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "takeover", true: "release"}[release], func(t *testing.T) {
			a, b, bare := stores(t)
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			a.Now = func() time.Time { return now }
			b.Now = func() time.Time { return now.Add(12 * time.Minute) }
			c := claim(t, a, "login", "one")
			gitTest(t, a.Dir, "checkout", "-b", "login")
			gitTest(t, a.Dir, "commit", "--allow-empty", "-m", "change")
			head := gitTest(t, a.Dir, "rev-parse", "HEAD")
			if err := a.Push(ctx, c, "login", "", head); err != nil {
				t.Fatal(err)
			}
			// An old holder may already have renewed before it pauses at Merge.
			if err := a.Renew(ctx, c); err != nil {
				t.Fatal(err)
			}
			var next string
			if release {
				if err := b.Release(ctx, "login", "two", "login", true); err != nil {
					t.Fatal(err)
				}
			} else {
				got, err := b.Acquire(ctx, "login", "two", "new", "login", true, false)
				if err != nil {
					t.Fatal(err)
				}
				next = got.BranchTip
			}
			tip := gitTest(t, bare, "rev-parse", "refs/heads/login")
			if tip == head || next != "" && tip != next {
				t.Fatal("branch not moved atomically")
			}
			// This is the forge's pinned merge contract: only the current tip can merge.
			if accepted := head == tip; accepted {
				t.Fatal("stale pinned merge accepted")
			}
			gitTest(t, a.Dir, "commit", "--allow-empty", "-m", "CI fix")
			fix := gitTest(t, a.Dir, "rev-parse", "HEAD")
			err := a.Push(ctx, c, "login", head, fix)
			if release {
				wantErr(t, err, "claim released")
			} else {
				wantErr(t, err, "claim taken by two")
			}
			if gitTest(t, bare, "rev-parse", "refs/heads/login") != tip {
				t.Fatal("zombie pushed")
			}
		})
	}
}
func TestOldHolderFirstPushAfterReleaseOrTakeover(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "takeover", true: "release"}[release], func(t *testing.T) {
			a, b, bare := stores(t)
			now := time.Now()
			a.Now = func() time.Time { return now }
			b.Now = func() time.Time { return now.Add(time.Hour) }
			c := claim(t, a, "login", "one")
			head := gitTest(t, a.Dir, "rev-parse", "HEAD")
			if release {
				if err := b.Release(ctx, "login", "two", "login", true); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := b.Acquire(ctx, "login", "two", "new", "login", true, false); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.Push(ctx, c, "login", "", head); err == nil {
				t.Fatal("stale holder created branch")
			}
			out, err := exec.Command("git", "-C", bare, "show-ref", "--verify", "refs/heads/login").CombinedOutput()
			if err == nil {
				t.Fatalf("branch created: %s", out)
			}
		})
	}
}
func TestTakeoverRetriesWhenBranchMoves(t *testing.T) {
	a, b, bare := stores(t)
	now := time.Now()
	a.Now = func() time.Time { return now }
	b.Now = func() time.Time { return now.Add(time.Hour) }
	c := claim(t, a, "login", "one")
	gitTest(t, a.Dir, "checkout", "-b", "login")
	gitTest(t, a.Dir, "commit", "--allow-empty", "-m", "change")
	head := gitTest(t, a.Dir, "rev-parse", "HEAD")
	if err := a.Push(ctx, c, "login", "", head); err != nil {
		t.Fatal(err)
	}
	var fixed string
	var once sync.Once
	b.beforePush = func() {
		once.Do(func() {
			gitTest(t, a.Dir, "commit", "--allow-empty", "-m", "fix")
			fixed = gitTest(t, a.Dir, "rev-parse", "HEAD")
			if err := a.Push(ctx, c, "login", head, fixed); err != nil {
				t.Fatal(err)
			}
		})
	}
	got, err := b.Acquire(ctx, "login", "two", "new", "login", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if gitTest(t, bare, "rev-parse", got.BranchTip+"^") != fixed {
		t.Fatal("takeover lost intervening push")
	}
}
func TestReleasePermissionAndEpochSurvivesRemoval(t *testing.T) {
	a, b, _ := stores(t)
	c := claim(t, a, "login", "one")
	wantErr(t, b.Release(ctx, "login", "two", "login", false), "--force")
	if err := a.Release(ctx, "login", "one", "login", false); err != nil {
		t.Fatal(err)
	}
	wantErr(t, a.Renew(ctx, c), "claim released")
	d := claim(t, b, "login", "two")
	if d.Epoch != c.Epoch+1 {
		t.Fatal("epoch reused after release")
	}
}
func TestPruneArchivedAndLocalRead(t *testing.T) {
	a, _, _ := stores(t)
	c := claim(t, a, "login", "one")
	gitTest(t, a.Dir, "checkout", "main")
	path := filepath.Join(a.Dir, "openspec", "changes", "archive", "2026-01-01-login")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "tasks.md"), []byte("done"), 0644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, a.Dir, "add", ".")
	gitTest(t, a.Dir, "commit", "-m", "archive")
	a.Base = "main"
	claim(t, a, "users", "one")
	all, err := a.Read(ctx)
	if err != nil || len(all) != 1 || all["users"].Change != "users" {
		t.Fatalf("%v %v", all, err)
	}
	wantErr(t, a.Renew(ctx, c), "claim released")
	local, err := a.ReadLocal(ctx)
	if err != nil || len(local) != 1 {
		t.Fatalf("local: %v %v", local, err)
	}
}
