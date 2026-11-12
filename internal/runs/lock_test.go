package runs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Pick: the change lock is one atomic try-lock; a second taker is refused
// while the first holds it, and gets it once it is released.
func TestChangeLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tuckerbox", "run.lock")
	if Active(p) {
		t.Fatal("active before any lock")
	}
	a, err := TryChangeLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !Active(p) {
		t.Error("not active while held")
	}
	if _, err := TryChangeLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, err := TryChangeLock(p)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	b.Release()
}

// TakeSlot: at most max slots are held at once, and a freed slot is taken.
func TestSlots(t *testing.T) {
	common := t.TempDir()
	var held []*Slot
	for i := 0; i < 2; i++ {
		s, err := TrySlot(common, 2)
		if err != nil || s == nil {
			t.Fatalf("slot %d: %v %v", i, s, err)
		}
		held = append(held, s)
	}
	if s, err := TrySlot(common, 2); err != nil || s != nil {
		t.Fatalf("third slot with max 2: %v %v", s, err)
	}
	if held[0].N == held[1].N {
		t.Error("same slot twice")
	}
	held[1].Release()
	s, err := TrySlot(common, 2)
	if err != nil || s == nil || s.N != held[1].N {
		t.Fatalf("freed slot not taken: %+v %v", s, err)
	}
	s.Release()
	held[0].Release()
}

// The queue lets only the oldest live ticket take a slot, and drops tickets
// whose run has died (its change lock is free).
func TestQueueOrderAndDeadTickets(t *testing.T) {
	common := t.TempDir()
	locks := map[string]*ChangeLock{}
	lockPath := func(c string) string { return filepath.Join(common, "wt-"+c, "run.lock") }
	join := func(c string, at int64) *Queue {
		l, err := TryChangeLock(lockPath(c))
		if err != nil {
			t.Fatal(err)
		}
		locks[c] = l
		q, err := JoinQueue(common, c, lockPath(c), at)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	qa, qb, qc := join("a", 1), join("b", 2), join("c", 3)
	if !qa.Head() || qb.Head() || qc.Head() {
		t.Fatal("a should be the only head")
	}
	// a's process dies: its lock is freed but its ticket stays.
	locks["a"].Release()
	if !qb.Head() {
		t.Error("b not head after a died")
	}
	if _, err := os.Stat(filepath.Join(QueueDir(common), qa.ticket)); err == nil {
		t.Error("dead ticket not removed")
	}
	qb.Leave()
	if !qc.Head() {
		t.Error("c not head after b left")
	}
	// A new ticket for c replaces the old one.
	locks["c"].Release()
	qc2 := join("c", 4)
	entries, _ := os.ReadDir(QueueDir(common))
	if len(entries) != 1 || entries[0].Name() != qc2.ticket {
		t.Errorf("queue %v", entries)
	}
	qc2.Leave()
	locks["c"].Release()
	locks["b"].Release()
}

func TestWaitSlotStops(t *testing.T) {
	common := t.TempDir()
	s, _ := TrySlot(common, 1)
	defer s.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	waited := false
	_, err := WaitSlot(ctx, common, 1, nil, 10*time.Millisecond, func() { waited = true })
	if !errors.Is(err, context.DeadlineExceeded) || !waited {
		t.Fatalf("err %v waited %v", err, waited)
	}
}

func TestUserStateDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	home := func() (string, error) { return filepath.Join("/", "home", "u"), nil }
	abs := filepath.Join(t.TempDir(), "xdg")
	cases := []struct {
		name string
		env  map[string]string
		goos string
		want string
	}{
		{"xdg", map[string]string{"XDG_STATE_HOME": abs}, "linux", filepath.Join(abs, "tuckerbox")},
		{"relative xdg ignored", map[string]string{"XDG_STATE_HOME": "rel"}, "darwin", filepath.Join("/", "home", "u", ".local", "state", "tuckerbox")},
		{"macos", nil, "darwin", filepath.Join("/", "home", "u", ".local", "state", "tuckerbox")},
		{"linux", nil, "linux", filepath.Join("/", "home", "u", ".local", "state", "tuckerbox")},
		{"windows", map[string]string{"LocalAppData": abs}, "windows", filepath.Join(abs, "tuckerbox")},
		{"windows xdg", map[string]string{"XDG_STATE_HOME": abs, "LocalAppData": "x"}, "windows", filepath.Join(abs, "tuckerbox")},
	}
	for _, c := range cases {
		got, err := UserStateDir(env(c.env), home, c.goos)
		if err != nil || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.name, got, err, c.want)
		}
	}
	if _, err := UserStateDir(env(nil), home, "windows"); err == nil {
		t.Error("windows without LocalAppData: no error")
	}
}

func TestWriteRunAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "run.json")
	for i := 1; i <= 3; i++ {
		if err := WriteRun(p, &Run{Change: "c", Round: i, State: Running}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := ReadRun(p)
	if err != nil || r.Round != 3 || r.Version != RunVersion {
		t.Fatalf("%+v %v", r, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
	if _, err := ReadRun(filepath.Join(dir, "none.json")); !errors.Is(err, ErrNoRun) {
		t.Errorf("missing run: %v", err)
	}
}

func TestEffective(t *testing.T) {
	r := &Run{State: Running, Pid: 42}
	if v := r.Effective(true); v.State != Running || v.Pid != 42 {
		t.Errorf("active: %+v", v)
	}
	if v := r.Effective(false); v.State != Stopped || !strings.Contains(v.Reason, "ended without") || v.Pid != 0 {
		t.Errorf("dead: %+v", v)
	}
	if v := (&Run{State: Blocked, Reason: "x"}).Effective(false); v.State != Blocked || v.Reason != "x" {
		t.Errorf("blocked: %+v", v)
	}
	if r.State != Running {
		t.Error("Effective changed the run")
	}
}
