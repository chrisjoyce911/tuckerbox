//go:build !windows

package cli

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// stopSignals end a run gracefully: the engine stops its agent and records
// the run as stopped.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// detach makes cmd a new session leader with no controlling terminal, so it
// outlives the terminal and the shell that started it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// afterHandshake is called in a detached engine once its parent has stopped
// reading its output: writes to the closed pipe must fail, not kill it.
func afterHandshake() { signal.Ignore(syscall.SIGPIPE) }

// signalStop asks the run process to stop.
func signalStop(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// killRun ends the run process and its agent's process group when it did
// not stop in time.
func killRun(pid, agentPid int) {
	if agentPid > 0 {
		_ = syscall.Kill(-agentPid, syscall.SIGKILL)
	}
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
