//go:build windows

package agent

import (
	"os/exec"
	"syscall"
)

// setProcAttr starts the agent in a new process group, detached from
// Tuckerbox's console signals.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// interrupt has no portable graceful form on Windows; the agent is killed.
// Processes the agent started are not killed with it (no job object yet).
func interrupt(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func kill(cmd *exec.Cmd) error { return cmd.Process.Kill() }
