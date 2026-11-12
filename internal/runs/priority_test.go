package runs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// queueFixture is a repository's common directory and a checkout holding
// change folders, with runs that hold their change locks as a queued run
// does. Each lock, slot and ticket is its own OS file lock, as in separate
// processes.
type queueFixture struct {
	t        *testing.T
	common   string
	checkout string
	locks    map[string]*ChangeLock
}

func newQueueFixture(t *testing.T, changes ...string) *queueFixture {
	t.Helper()
	f := &queueFixture{t: t, common: t.TempDir(), checkout: t.TempDir(), locks: map[string]*ChangeLock{}}
	for _, c := range changes {
		testenv.WriteFile(t, filepath.Join(f.checkout, "openspec", "changes", c, "proposal.md"), "# "+c+"\n")
	}
	t.Cleanup(func() {
		for _, l := range f.locks {
			l.Release()
		}
	})
	return f
}

func (f *queueFixture) lockPath(c string) string { return filepath.Join(f.common, "wt-"+c, "run.lock") }

func (f *queueFixture) join(c string, at int64) *Queue {
	f.t.Helper()
	if f.locks[c] == nil {
		l, err := TryChangeLock(f.lockPath(c))
		if err != nil {
			f.t.Fatal(err)
		}
		f.locks[c] = l
	}
	q, err := JoinQueue(f.common, f.checkout, c, f.lockPath(c), at)
	if err != nil {
		f.t.Fatal(err)
	}
	return q
}

// setPriority writes c's priority in the checkout, as tuckerbox priority
// does, without taking the lock (the caller decides).
func (f *queueFixture) setPriority(c string, v int64) {
	f.t.Helper()
	p := priority.File(f.checkout, c)
	old, _ := os.ReadFile(p)
	data, err := priority.SetValue(old, v)
	if err != nil {
		f.t.Fatal(err)
	}
	testenv.WriteFile(f.t, p, string(data))
}

// waitAdmit runs WaitSlot for q in the background.
func waitAdmit(ctx context.Context, q *Queue, common string, max int) <-chan *Slot {
	ch := make(chan *Slot, 1)
	go func() {
		s, _ := WaitSlot(ctx, common, max, q, 5*time.Millisecond, nil)
		ch <- s
	}()
	return ch
}

func noSlot(t *testing.T, what string, ch <-chan *Slot, d time.Duration) {
	t.Helper()
	select {
	case s := <-ch:
		if s != nil {
			s.Release()
		}
		t.Fatalf("%s took a slot", what)
	case <-time.After(d):
	}
}

func gotSlot(t *testing.T, what string, ch <-chan *Slot) *Slot {
	t.Helper()
	select {
	case s := <-ch:
		if s == nil {
			t.Fatalf("%s: no slot", what)
		}
		return s
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not take the slot", what)
	}
	return nil
}

// The queue's head follows the checkout's priorities, live: a change moved
// to the top is first at the next check; without priorities the earliest
// request is.
func TestQueueHeadFollowsPriority(t *testing.T) {
	f := newQueueFixture(t, "b", "c")
	qb, qc := f.join("b", 1), f.join("c", 2)
	if !qb.Head() || qc.Head() {
		t.Fatal("b, requested first, should be the head")
	}
	f.setPriority("c", 1000)
	if qb.Head() || !qc.Head() {
		t.Fatal("c, ranked, should be the head")
	}
	f.setPriority("b", 500)
	if !qb.Head() {
		t.Fatal("b, ranked higher, should be the head")
	}
	// A dependent waiting outside the queue lends its rank (Dependency moves up).
	testenv.WriteFile(t, filepath.Join(f.checkout, "openspec", "changes", "d", "tuckerbox.toml"), "priority = 1\ndepends_on = [\"c\"]\n")
	if !qc.Head() {
		t.Fatal("c should inherit d's rank")
	}
}

// Scenario: Reorder as a slot frees. B is first in the queue; A ends, and
// a priority move moving C to the top finishes before B takes the slot: C
// takes it and B stays queued.
func TestReorderAsASlotFrees(t *testing.T) {
	f := newQueueFixture(t, "a", "b", "c")
	a, err := TrySlot(f.common, 1)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	qb, qc := f.join("b", 1), f.join("c", 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, c := waitAdmit(ctx, qb, f.common, 1), waitAdmit(ctx, qc, f.common, 1)
	noSlot(t, "b while a runs", b, 50*time.Millisecond)

	// The move holds the sidecar lock for its writes; A ends meanwhile.
	move, err := LockSidecar(f.common)
	if err != nil {
		t.Fatal(err)
	}
	a.Release()
	f.setPriority("c", 1000)
	move.Release()

	s := gotSlot(t, "c", c)
	defer s.Release()
	noSlot(t, "b", b, 100*time.Millisecond)
}

// Scenario: Higher-priority arrival during admission. A holds the only
// slot, B finds itself first, and C, ranked above B, is started as A
// ends: C's ticket can't land inside B's check, so B never takes the slot
// while C is queued.
func TestHigherPriorityArrivalDuringAdmission(t *testing.T) {
	f := newQueueFixture(t, "a", "b", "c")
	f.setPriority("c", 1000)
	a, err := TrySlot(f.common, 1)
	if err != nil || a == nil {
		t.Fatal(err)
	}
	cl, err := TryChangeLock(f.lockPath("c"))
	if err != nil {
		t.Fatal(err)
	}
	f.locks["c"] = cl
	qb := f.join("b", 1)

	var once sync.Once
	joined := make(chan *Queue, 1)
	inCheck := make(chan bool, 1) // whether C's ticket existed during B's held check
	hook := func(change string, first bool) {
		if change != "b" || !first {
			return
		}
		once.Do(func() {
			// Between B's listing and its slot try: C is started and A ends.
			go func() {
				q, err := JoinQueue(f.common, f.checkout, "c", f.lockPath("c"), 2)
				if err != nil {
					t.Error(err)
				}
				joined <- q
			}()
			a.Release()
			time.Sleep(100 * time.Millisecond)
			entries, _ := os.ReadDir(QueueDir(f.common))
			inCheck <- len(entries) == 2
		})
	}
	admitHook.Store(&hook)
	defer admitHook.Store(nil)

	s, err := qb.TryAdmit(1)
	if err != nil {
		t.Fatal(err)
	}
	if <-inCheck {
		t.Fatal("C's ticket was published during B's admission check")
	}
	if s == nil {
		t.Fatal("B, first when it checked, did not get the free slot")
	}
	// C joined after B took the slot: C is now first, and waits for it.
	qc := <-joined
	if !qc.Head() {
		t.Error("c should be first once queued")
	}
	if got, _ := qc.TryAdmit(1); got != nil {
		t.Error("c took a second slot")
	}
	s.Release()
	if got, _ := qc.TryAdmit(1); got == nil {
		t.Error("c did not take the freed slot")
	} else {
		got.Release()
	}
}

// Scenario: Slot frees during a renumber. A move rewrites five files and a
// slot frees while it writes them: no run takes the slot until the move
// ends, and then the first run in the new order takes it.
func TestSlotFreesDuringARenumber(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	f := newQueueFixture(t, append(ids, "run")...)
	for i, id := range ids {
		f.setPriority(id, int64(1000+i)) // a first, no gaps
	}
	r, err := TrySlot(f.common, 1)
	if err != nil || r == nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chans := map[string]<-chan *Slot{}
	for i, id := range ids {
		chans[id] = waitAdmit(ctx, f.join(id, int64(i+1)), f.common, 1)
	}
	// Stop the waiters that are still waiting, and wait for them to return,
	// before the temporary folders are removed: a waiter still writing its
	// queue files makes the cleanup fail with "directory not empty".
	defer func() {
		cancel()
		for _, id := range ids[:4] {
			select { // a channel a failed check already read stays empty
			case s := <-chans[id]:
				if s != nil {
					s.Release()
				}
			case <-time.After(5 * time.Second):
			}
		}
	}()
	// The renumber reverses the order: e, d, c, b, a.
	move, err := LockSidecar(f.common)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		f.setPriority(id, int64(5000-1000*i))
		if i == 1 {
			r.Release() // the slot frees halfway through
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, id := range ids {
		select {
		case s := <-chans[id]:
			t.Fatalf("%s took the slot during the move (%v)", id, s)
		default:
		}
	}
	move.Release()
	s := gotSlot(t, "e", chans["e"])
	defer s.Release()
	for _, id := range ids[:4] {
		noSlot(t, id, chans[id], 20*time.Millisecond)
	}
}

// Scenario: Reorder while queued. max_parallel = 1, A runs, B and then C
// are queued, and C is moved to the top: C starts when A ends, before B.
func TestReorderWhileQueued(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}})
	gate := f.gate("a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: gate}})
	da := f.start(context.Background(), Request{Change: "a"})
	f.waitState("a", Running, "build")
	db := f.start(context.Background(), Request{Change: "b"})
	dc := f.start(context.Background(), Request{Change: "c"})
	f.waitState("b", Queued, "")
	f.waitState("c", Queued, "")

	// tuckerbox priority c top, in the checkout, under the sidecar lock.
	err := WithSidecarLock(f.repo.Common, func() error {
		cs, err := priority.Load(f.repo.Checkout())
		if err != nil {
			return err
		}
		m, err := priority.Plan(cs, "c", priority.Target{Rel: "top"})
		if err != nil {
			return err
		}
		for id, v := range m.Values {
			p := priority.File(f.repo.Checkout(), id)
			old, _ := os.ReadFile(p)
			data, err := priority.SetValue(old, v)
			if err != nil {
				return err
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	open_(t, gate)
	for _, ch := range []<-chan result{da, dc, db} {
		if res := <-ch; res.err != nil || res.run.State != Ready {
			t.Fatalf("%+v %v", res.run, res.err)
		}
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "c build 1", "c review 1", "b build 1", "b review 1")
}

// Scenario (change-runs): Unranked changes keep request order. With no
// priorities, B then C requested while A runs: B starts before C, whatever
// their ages.
func TestUnrankedQueueKeepsRequestOrder(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}})
	// C is older than B.
	for id, d := range map[string]string{"b": "2026-02-01", "c": "2026-01-01"} {
		testenv.WriteFile(t, filepath.Join(f.dir, "openspec", "changes", id, ".openspec.yaml"), "created: "+d+"\n")
	}
	gate := f.gate("a")
	f.env.SetPlan(t, map[string]testenv.Action{"a:build": {WaitFor: gate}})
	da := f.start(context.Background(), Request{Change: "a"})
	f.waitState("a", Running, "build")
	db := f.start(context.Background(), Request{Change: "b"})
	dc := f.start(context.Background(), Request{Change: "c"})
	f.waitState("c", Queued, "")
	open_(t, gate)
	for _, ch := range []<-chan result{da, db, dc} {
		if res := <-ch; res.err != nil || res.run.State != Ready {
			t.Fatalf("%+v %v", res.run, res.err)
		}
	}
	wantStarts(t, f.env, "a build 1", "a review 1", "b build 1", "b review 1", "c build 1", "c review 1")
}
