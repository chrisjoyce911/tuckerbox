// Package machines runs Tuckerbox on the other machines listed in machine
// config ([machines.<name>]) and reads what each machine reports about
// itself (tuckerbox machine --json).
//
// A machine is reached with ssh or herdr-core:
//
//	ssh -T -o BatchMode=yes <destination> -- tuckerbox <args>
//	hc run --on <name> --rm -- tuckerbox <args>
//
// without a terminal and without asking for a password. No token or
// credential is sent: the arguments are Tuckerbox's own, and forge token
// variables are removed from the environment ssh and hc are started with.
package machines

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// DefaultReportTimeout is how long a machine report may take before the
// machine counts as unreachable.
const DefaultReportTimeout = 30 * time.Second

// ErrUnreachable is wrapped by the errors [Run] returns when the machine
// could not be reached or did not answer in time, as opposed to a
// Tuckerbox command that ran there and failed.
var ErrUnreachable = errors.New("unreachable")

// Program is the Tuckerbox command run on the other machine.
const Program = "tuckerbox"

// Runner runs commands on other machines. The zero value uses ssh and hc
// from PATH.
type Runner struct {
	// SSH and HC are the programs used; "" means "ssh" and "hc".
	SSH, HC string
	// Environ is the environment ssh and hc start with; nil means
	// os.Environ(). Token variables are removed from it either way.
	Environ []string
}

// Error is a command that ran on the machine and failed.
type Error struct {
	Machine string
	Stderr  string
	Err     error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Machine, msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Run runs tuckerbox with args on machine e and returns its standard
// output. A timeout of 0 means [DefaultReportTimeout]. The error wraps
// [ErrUnreachable] when the machine could not be reached, named by e.Name.
func Run(ctx context.Context, e config.MachineEntry, args []string, timeout time.Duration) ([]byte, error) {
	return Runner{}.Run(ctx, e, args, timeout)
}

// Command returns the program and arguments that run tuckerbox with args on
// machine e.
func (r Runner) Command(e config.MachineEntry, args []string) ([]string, error) {
	switch {
	case e.SSH != "" && e.HC != "":
		return nil, fmt.Errorf("machines.%s: give one of ssh or hc, not both", e.Name)
	case e.HC != "":
		hc := r.HC
		if hc == "" {
			hc = "hc"
		}
		return append([]string{hc, "run", "--on", e.HC, "--rm", "--", Program}, args...), nil
	case e.SSH != "":
		if strings.HasPrefix(e.SSH, "-") {
			return nil, fmt.Errorf("machines.%s: %q is not an SSH destination", e.Name, e.SSH)
		}
		ssh := r.SSH
		if ssh == "" {
			ssh = "ssh"
		}
		// ssh joins the remote command into one line for the remote
		// shell, so each argument is quoted for it.
		remote := []string{Program}
		for _, a := range args {
			remote = append(remote, shellQuote(a))
		}
		return []string{ssh, "-T", "-o", "BatchMode=yes", e.SSH, "--", strings.Join(remote, " ")}, nil
	}
	return nil, fmt.Errorf("machines.%s: give ssh or hc", e.Name)
}

// Run is [Run] with this runner's programs and environment.
func (r Runner) Run(ctx context.Context, e config.MachineEntry, args []string, timeout time.Duration) ([]byte, error) {
	argv, err := r.Command(e, args)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = DefaultReportTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = withoutTokens(r.Environ)
	cmd.Stdin = nil
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	switch {
	case err == nil:
		return stdout.Bytes(), nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("%s: no answer within %s: %w", e.Name, timeout, ErrUnreachable)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		// ssh or hc did not start.
		return nil, fmt.Errorf("%s: %v: %w", e.Name, err, ErrUnreachable)
	}
	if unreachableExit(e, exit.ExitCode(), stderr.String()) {
		return nil, fmt.Errorf("%s: %s: %w", e.Name, firstLine(stderr.String(), err), ErrUnreachable)
	}
	return stdout.Bytes(), &Error{Machine: e.Name, Stderr: stderr.String(), Err: err}
}

// unreachableExit reports whether a failed ssh or hc means the machine was
// not reached rather than that tuckerbox failed there. ssh exits 255 on a
// connection error; a remote shell without tuckerbox exits 127.
func unreachableExit(e config.MachineEntry, code int, stderr string) bool {
	if e.SSH != "" {
		return code == 255 || code == 127
	}
	s := strings.ToLower(stderr)
	return code == 127 || strings.Contains(s, "unknown machine") || strings.Contains(s, "not connected") || strings.Contains(s, "unreachable")
}

func firstLine(s string, err error) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return err.Error()
	}
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@,+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tokenVars are environment variables that hold forge tokens. They are not
// passed to ssh or hc, so no SendEnv or herdr-core forwarding can carry
// them to another machine.
var tokenVars = []string{"GITHUB_TOKEN", "GH_TOKEN", "GITLAB_TOKEN", "GL_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "CI_JOB_TOKEN"}

func withoutTokens(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if isTokenVar(k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isTokenVar(k string) bool {
	for _, t := range tokenVars {
		if strings.EqualFold(k, t) {
			return true
		}
	}
	u := strings.ToUpper(k)
	return strings.HasPrefix(u, "TUCKERBOX_") && strings.HasSuffix(u, "TOKEN")
}
