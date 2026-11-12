package approval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// clones makes a bare remote and n clones of it, each with one commit.
func clones(t *testing.T, n int) (remote string, out []string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", remote)
	seed := filepath.Join(root, "seed")
	gitT(t, root, "init", "-q", "-b", "main", seed)
	for _, kv := range [][2]string{{"user.name", "Test"}, {"user.email", "t@example.invalid"}, {"commit.gpgsign", "false"}} {
		gitT(t, seed, "config", kv[0], kv[1])
	}
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, seed, "add", "-A")
	gitT(t, seed, "commit", "-q", "-m", "seed")
	gitT(t, seed, "remote", "add", "origin", remote)
	gitT(t, seed, "push", "-q", "origin", "main")
	for i := 0; i < n; i++ {
		c := filepath.Join(root, "clone"+string(rune('a'+i)))
		gitT(t, root, "clone", "-q", remote, c)
		out = append(out, c)
	}
	return remote, out
}

func remoteEntries(t *testing.T, remote string) map[string]Entry {
	t.Helper()
	ctx := context.Background()
	es, err := Store{Dir: remote}.Entries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func approvedEntry(hash, by string) Entry {
	return Entry{State: EntryApproved, Hash: hash, By: by, At: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
}

// approval-publish: Approve from the board (the store records and publishes).
func TestRecordPublishesAtOnce(t *testing.T) {
	ctx := context.Background()
	remote, cs := clones(t, 1)
	if err := (Store{Dir: cs[0]}).Record(ctx, "add-login", approvedEntry("sha256:aa", "Owner")); err != nil {
		t.Fatal(err)
	}
	es := remoteEntries(t, remote)
	if es["add-login"].State != EntryApproved || es["add-login"].Hash != "sha256:aa" || es["add-login"].By != "Owner" {
		t.Fatalf("remote entries = %+v", es)
	}
	if st := gitT(t, cs[0], "status", "--porcelain"); st != "" {
		t.Fatalf("checkout changed: %q", st)
	}
	if un, _ := (Store{Dir: cs[0]}).Unpublished(ctx); len(un) != 0 {
		t.Fatalf("unpublished = %v", un)
	}
}

// approval-publish: Two approvals at once. Machine B builds its commit,
// then machine A pushes before B does; B's lease is refused, B fetches and
// pushes again, and both approvals are on the remote.
func TestTwoApprovalsAtOnce(t *testing.T) {
	ctx := context.Background()
	remote, cs := clones(t, 2)
	a, b := Store{Dir: cs[0]}, Store{Dir: cs[1]}
	pushes := 0
	b.beforePush = func() {
		pushes++
		if pushes == 1 {
			if err := a.Record(ctx, "add-login", approvedEntry("sha256:aa", "Studio")); err != nil {
				t.Fatal(err)
			}
		}
	}
	b.Sleep = func(time.Duration) {}
	if err := b.Record(ctx, "add-users", approvedEntry("sha256:bb", "Mini")); err != nil {
		t.Fatal(err)
	}
	if pushes != 2 {
		t.Fatalf("B pushed %d times, want 2 (one refused by the lease)", pushes)
	}
	es := remoteEntries(t, remote)
	if es["add-login"].Hash != "sha256:aa" || es["add-users"].Hash != "sha256:bb" {
		t.Fatalf("remote entries = %+v, want both approvals", es)
	}
}

// A withdrawal is an entry, and the latest operation for a change wins.
func TestWithdrawalReplacesApproval(t *testing.T) {
	ctx := context.Background()
	remote, cs := clones(t, 1)
	s := Store{Dir: cs[0]}
	if err := s.Record(ctx, "add-users", approvedEntry("sha256:bb", "Owner")); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "add-users", Entry{State: EntryWithdrawn, By: "Owner", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if es := remoteEntries(t, remote); es["add-users"].State != EntryWithdrawn {
		t.Fatalf("remote entries = %+v", es)
	}
}

// A repository with no remote keeps its approvals on the local ref.
func TestNoRemoteKeepsLocalRef(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "main")
	s := Store{Dir: dir}
	for _, c := range []string{"one", "two"} {
		if err := s.Record(ctx, c, approvedEntry("sha256:"+c, "Owner")); err != nil {
			t.Fatal(err)
		}
	}
	es, err := s.Entries(ctx)
	if err != nil || es["one"].State != EntryApproved || es["two"].State != EntryApproved {
		t.Fatalf("entries = %+v, %v", es, err)
	}
	if un, _ := s.Unpublished(ctx); len(un) != 0 {
		t.Fatalf("unpublished = %v", un)
	}
}

// Another machine's approval reaches this machine with Sync.
func TestSyncFetchesOtherMachines(t *testing.T) {
	ctx := context.Background()
	_, cs := clones(t, 2)
	if err := (Store{Dir: cs[0]}).Record(ctx, "add-login", approvedEntry("sha256:aa", "Studio")); err != nil {
		t.Fatal(err)
	}
	b := Store{Dir: cs[1]}
	if err := b.Sync(ctx, 0); err != nil {
		t.Fatal(err)
	}
	es, err := b.Entries(ctx)
	if err != nil || es["add-login"].Hash != "sha256:aa" {
		t.Fatalf("entries = %+v, %v", es, err)
	}
}

// approval-publish: Approve on a train. The remote can't be reached: the
// approval counts here and is unpublished; once the remote is back, Sync
// publishes it on top of what others pushed meanwhile.
func TestOfflineApprovalPublishesLater(t *testing.T) {
	ctx := context.Background()
	remote, cs := clones(t, 2)
	a := Store{Dir: cs[0]}
	gitT(t, cs[0], "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	err := a.Record(ctx, "add-login", approvedEntry("sha256:aa", "Owner"))
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("Record offline = %v, want ErrOffline", err)
	}
	if es, _ := a.Entries(ctx); es["add-login"].State != EntryApproved {
		t.Fatalf("local entries = %+v", es)
	}
	if un, _ := a.Unpublished(ctx); !un["add-login"] {
		t.Fatalf("unpublished = %v", un)
	}
	if err := a.Sync(ctx, 0); err != nil {
		t.Fatalf("Sync offline = %v, want nil", err)
	}
	// Meanwhile another machine approves something else.
	if err := (Store{Dir: cs[1]}).Record(ctx, "add-users", approvedEntry("sha256:bb", "Mini")); err != nil {
		t.Fatal(err)
	}
	gitT(t, cs[0], "remote", "set-url", "origin", remote)
	if err := a.Sync(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	es := remoteEntries(t, remote)
	if es["add-login"].Hash != "sha256:aa" || es["add-users"].Hash != "sha256:bb" {
		t.Fatalf("remote entries = %+v", es)
	}
	if un, _ := a.Unpublished(ctx); len(un) != 0 {
		t.Fatalf("unpublished after sync = %v", un)
	}
}

// approval-publish: Reading approvals: a ref entry wins over the sidecar,
// a withdrawal is None, and without an entry the sidecar counts.
func TestJudgeWith(t *testing.T) {
	d := copyFixture(t, false)
	hash, err := Hash(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(d, "Old", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := JudgeWith(d, nil)
	if err != nil || st.State != Approved || st.FromRef {
		t.Fatalf("no entry: %+v, %v", st, err)
	}
	st, _ = JudgeWith(d, &Entry{State: EntryWithdrawn})
	if st.State != None || !st.FromRef {
		t.Fatalf("withdrawn: %+v", st)
	}
	st, _ = JudgeWith(d, &Entry{State: EntryApproved, Hash: hash, By: "New"})
	if st.State != Approved || st.Approval.By != "New" {
		t.Fatalf("approved entry: %+v", st)
	}
	st, _ = JudgeWith(d, &Entry{State: EntryApproved, Hash: "sha256:old"})
	if st.State != Stale {
		t.Fatalf("edited after approval: %+v", st)
	}
}
