package fleetslot

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

// With FLEETSLOT_HOLD set, the test binary takes a slot, says so and holds
// it until its standard input closes: another process.
func TestMain(m *testing.M) {
	if home := os.Getenv("FLEETSLOT_HOLD"); home != "" {
		s, err := Take(home, "mini", 1)
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Println("held")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		_ = s.Free()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestTakeAndFree(t *testing.T) {
	home := t.TempDir()
	a, err := Take(home, "mini", 2)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Take(home, "mini", 2)
	if err != nil || a.N == b.N {
		t.Fatalf("second slot: %v %v", b, err)
	}
	if n, err := InUse(home, 2); err != nil || n != 2 {
		t.Fatalf("in use %d %v", n, err)
	}
	_, err = Take(home, "mini", 2)
	if !errors.Is(err, ErrFull) || err.Error() != "mini is full" {
		t.Fatalf("third take: %v", err)
	}
	if err := a.Free(); err != nil {
		t.Fatal(err)
	}
	if err := a.Free(); err != nil {
		t.Fatal("freeing twice")
	}
	if n, _ := InUse(home, 2); n != 1 {
		t.Fatalf("in use after free %d", n)
	}
	c, err := Take(home, "mini", 2)
	if err != nil {
		t.Fatal(err)
	}
	c.Free()
	b.Free()
}

func TestNoCapacity(t *testing.T) {
	if _, err := Take(t.TempDir(), "laptop", 0); !errors.Is(err, ErrFull) {
		t.Fatalf("err %v", err)
	}
}

// Another process holding the only slot makes this one full; once that
// process ends, the slot is free again (a crashed run frees its slot).
func TestSlotHeldByAnotherProcess(t *testing.T) {
	home := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "FLEETSLOT_HOLD="+home)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if line != "held\n" {
		t.Fatalf("holder said %q", line)
	}
	if _, err := Take(home, "mini", 1); !errors.Is(err, ErrFull) {
		t.Fatalf("take while another process holds the slot: %v", err)
	}
	if n, _ := InUse(home, 1); n != 1 {
		t.Fatalf("in use %d", n)
	}
	in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s, err := Take(home, "mini", 1)
		if err == nil {
			s.Free()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot not freed after the holder ended: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
