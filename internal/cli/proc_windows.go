//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

// stopSignals end a run gracefully when it runs in the foreground (Ctrl-C).
// A detached run is stopped through its stop file instead.
var stopSignals = []os.Signal{os.Interrupt}

// Process creation flags (winbase.h).
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// detach starts cmd without a console and in its own process group, so it
// outlives the console window that started it and gets none of its Ctrl-C.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

func afterHandshake() {}

// signalStop has no signal to send on Windows: tuckerbox stop writes the
// run's stop file, which the engine polls.
func signalStop(pid int) error { return nil }

// killRun kills the run process and its agent when they did not stop in
// time. Processes the agent started keep running (no job object yet; see
// design.md, Risks).
func killRun(pid, agentPid int) {
	for _, p := range []int{agentPid, pid} {
		if p <= 0 {
			continue
		}
		if proc, err := os.FindProcess(p); err == nil {
			_ = proc.Kill()
		}
	}
}
