package runs

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

// Admission takes a slot only when its run lock and its agent lock are
// both free (formal/liveslots.tla, Admit): an orphaned agent's slot stays
// taken after its run process is gone.
func TestAdmissionNeedsBothSlotLocks(t *testing.T) {
	common := t.TempDir()
	orphan, err := HoldAgentLock(common, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := TrySlot(common, 1)
	if err != nil || s != nil {
		t.Fatalf("admitted to slot %v (%v) while an agent holds slot 0's agent lock", s, err)
	}
	s, err = TrySlot(common, 2)
	if err != nil || s == nil || s.N != 1 {
		t.Fatalf("slot %v (%v), want 1", s, err)
	}
	// The test of slot 0 let its run lock go again.
	l := flock.New(SlotLock(common, 0))
	if ok, _ := l.TryLock(); !ok {
		t.Error("admission kept slot 0's run lock")
	}
	_ = l.Unlock()
	s.Release()
	if err := orphan.Release(); err != nil {
		t.Fatal(err)
	}
	s, err = TrySlot(common, 1)
	if err != nil || s == nil || s.N != 0 {
		t.Fatalf("slot %v (%v) after the agent ended, want 0", s, err)
	}
	// Admission only tests the agent lock: the admitted run's own agent
	// can take it.
	h, err := HoldAgentLock(common, 0, 0)
	if err != nil {
		t.Fatalf("the admitted run's agent can't take its slot's agent lock: %v", err)
	}
	h.Release()
	s.Release()
}

func TestHoldAgentLockRefusesWhenHeld(t *testing.T) {
	common := t.TempDir()
	a, err := HoldAgentLock(common, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := HoldAgentLock(common, 2, 200*time.Millisecond); err != ErrAgentLockHeld {
		t.Errorf("second hold: %v", err)
	}
	if time.Since(start) < 200*time.Millisecond {
		t.Error("gave up before its wait")
	}
	// One that is just exiting lets go within the wait.
	go func() {
		time.Sleep(100 * time.Millisecond)
		a.Release()
	}()
	b, err := HoldAgentLock(common, 2, 2*time.Second)
	if err != nil {
		t.Fatalf("hold after the first let go: %v", err)
	}
	b.Release()
}

// Adoption takes the run lock of the agent's own slot, needing no free
// slot and not testing the agent lock it holds, and waits out an
// admission's brief hold (liveslots.tla, Adopt).
func TestTakeOwnSlot(t *testing.T) {
	common := t.TempDir()
	agentLock, err := HoldAgentLock(common, 1, 0) // the orphan's agent
	if err != nil {
		t.Fatal(err)
	}
	defer agentLock.Release()
	busy, err := TrySlot(common, 1) // slot 0 taken by another run
	if err != nil || busy == nil || busy.N != 0 {
		t.Fatalf("slot %v %v", busy, err)
	}
	defer busy.Release()
	// An admission testing slot 1 holds its run lock for a moment.
	probe := flock.New(SlotLock(common, 1))
	if ok, _ := probe.TryLock(); !ok {
		t.Fatal("probe")
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = probe.Unlock()
	}()
	start := time.Now()
	s, err := TakeOwnSlot(context.Background(), common, 1, 20*time.Millisecond, func() bool { return true })
	if err != nil || s == nil || s.N != 1 {
		t.Fatalf("TakeOwnSlot: %v %v", s, err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Error("took the slot while an admission held its run lock")
	}
	s.Release()

	// An agent that ends meanwhile: adoption gives up.
	if ok, _ := probe.TryLock(); !ok {
		t.Fatal("probe")
	}
	defer probe.Unlock()
	s, err = TakeOwnSlot(context.Background(), common, 1, 20*time.Millisecond, func() bool { return false })
	if err != nil || s != nil {
		t.Errorf("TakeOwnSlot with the agent gone: %v %v", s, err)
	}
}

// A running run whose process died is shown stopped, unless its agent
// lives on in a terminal: then running, not watched.
func TestRunViewNotWatched(t *testing.T) {
	r := &Run{Change: "add-login", State: Running, Step: "build", Round: 2, Pid: 9, Terminal: &TerminalRef{Name: "tb-x"}}
	if v, watched := r.View(false, true); v.State != Running || watched || v.Reason != NotWatched || v.Pid != 0 {
		t.Errorf("orphan with a live agent: %s %q watched=%v", v.State, v.Reason, watched)
	}
	if v, watched := r.View(false, false); v.State != Stopped || !watched || v.Reason != Crashed {
		t.Errorf("orphan whose agent ended: %s %q", v.State, v.Reason)
	}
	if v, watched := r.View(true, true); v.State != Running || !watched || v.Reason != "" {
		t.Errorf("watched run: %s %q", v.State, v.Reason)
	}
	headless := &Run{Change: "add-login", State: Running}
	if v, _ := headless.View(false, true); v.State != Stopped {
		t.Errorf("headless crash: %s", v.State)
	}
}
