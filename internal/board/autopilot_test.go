package board

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/autopilot"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// writeMarker writes an autopilot marker for the repository at root and
// returns its lock's path.
func writeMarker(t *testing.T, root string, m autopilot.Marker) string {
	t.Helper()
	repo, err := runs.OpenRepo(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repo.Common, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, marker := autopilot.Files(repo.Common)
	data, _ := json.Marshal(m)
	if err := os.WriteFile(marker, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return lock
}

// Scenario: Autopilot running (board-data: Autopilot in the board document).
func TestAutopilotRunning(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"a"}, RequireApproval: true})
	started := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	lockPath := writeMarker(t, root, autopilot.Marker{Pid: 4242, Machine: "mini", Started: started, Terminal: "tb-ap-3f2a9c1d0b4e"})
	lock, err := runs.TryChangeLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	got := read(t, root, "").Autopilot
	want := Autopilot{Running: true, Pid: 4242, Machine: "mini", Started: timestamp(started), Terminal: "tb-ap-3f2a9c1d0b4e"}
	if got != want {
		t.Errorf("autopilot %+v, want %+v", got, want)
	}
	// Reading it started nothing and left the lock with its holder.
	if !runs.Active(lockPath) {
		t.Error("the lock is no longer held")
	}
}

// Scenario: Marker left by a crash: a marker with no lock holder is not a
// running autopilot.
func TestAutopilotMarkerLeftByCrash(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"a"}, RequireApproval: true})
	lockPath := writeMarker(t, root, autopilot.Marker{Pid: 4242, Machine: "mini", Started: time.Now(), Terminal: "tb-ap-3f2a9c1d0b4e"})
	// The dead autopilot's lock file is still there, unheld.
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "").Autopilot; got != (Autopilot{}) {
		t.Errorf("autopilot %+v, want not running", got)
	}
	data, _ := json.Marshal(read(t, root, "").Autopilot)
	if string(data) != `{"running":false}` {
		t.Errorf("json %s", data)
	}
}
