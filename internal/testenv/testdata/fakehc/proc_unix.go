//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// startHost starts this program as the detached host of terminal name, in
// a session of its own, and returns its pid.
func startHost(name string) (int, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(self, "host", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}

// launchLater starts this program, detached, to run "run args" after d.
func launchLater(d time.Duration, args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, append([]string{"run"}, args...)...)
	cmd.Env = append(os.Environ(), "FAKEHC_LATER="+d.String())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// host runs the terminal's program: its input is what send appends to
// <name>.in, its output goes to <name>.out. It waits for it and records
// how it ended.
func host(name string) {
	// Wait until run has written the record (it holds the lock).
	unlock := lock()
	r, err := readRecord(name)
	unlock()
	must(err)
	out, err := os.OpenFile(path(name, ".out"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	must(err)
	cmd := exec.Command(r.Argv[0], r.Argv[1:]...)
	cmd.Dir = r.Cwd
	cmd.Stdout, cmd.Stderr = out, out
	stdin, err := cmd.StdinPipe()
	must(err)
	// A hangup ends the program; the host stays to record that.
	signal.Ignore(syscall.SIGPIPE)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP, syscall.SIGTERM)
	exit := -1
	if err := cmd.Start(); err != nil {
		out.WriteString("fakehc: " + err.Error() + "\n")
		exit = 127
	} else {
		go feed(path(name, ".in"), stdin)
		err := cmd.Wait()
		exit = cmd.ProcessState.ExitCode()
		var ee *exec.ExitError
		if exit < 0 && errors.As(err, &ee) {
			exit = 129
		}
	}
	out.Close()
	unlock = lock()
	defer unlock()
	if cur, err := readRecord(name); err == nil && cur.Pid == os.Getpid() {
		cur.State, cur.Exit = "finished", &exit
		writeRecord(cur)
	}
}

// feed copies what is appended to the input file to the program.
func feed(p string, w io.WriteCloser) {
	f, err := os.Open(p)
	if err != nil {
		return
	}
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err == io.EOF || n == 0 {
			time.Sleep(30 * time.Millisecond)
			continue
		}
		if err != nil {
			return
		}
	}
}

func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func hangup(pid int) { _ = syscall.Kill(-pid, syscall.SIGHUP) }

func killGroup(pid int) { _ = syscall.Kill(-pid, syscall.SIGKILL) }
