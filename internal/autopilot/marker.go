package autopilot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// Marker is autopilot.json: who holds autopilot.lock. It counts
// only while that lock is held.
type Marker struct {
	Pid     int       `json:"pid"`
	Machine string    `json:"machine"`
	Started time.Time `json:"started"`
}

// Files are the lock and marker of a repository's autopilot, in
// the shared tuckerbox folder of its git common directory.
func Files(common string) (lock, marker string) {
	return filepath.Join(common, "autopilot.lock"), filepath.Join(common, "autopilot.json")
}

// ReadMarker returns the running autopilot of the repository whose
// tuckerbox folder is common, or nil when none holds the lock.
func ReadMarker(common string) *Marker {
	lock, marker := Files(common)
	if !runs.Active(lock) {
		return nil
	}
	m := &Marker{}
	if data, err := os.ReadFile(marker); err == nil {
		_ = json.Unmarshal(data, m)
	}
	return m
}
