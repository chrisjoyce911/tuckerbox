// Command fakessh stands in for ssh in tuckerbox dispatch tests, on every
// OS. It is called as
//
//	ssh -T -o BatchMode=yes <dest> -- <remote command line>
//
// and appends its arguments, as one JSON array, to <dir>/calls.log (and
// its environment to <dir>/env.log), where
// <dir> is "fakessh" beside $XDG_STATE_HOME (the remote runner passes only
// a clean environment, and XDG_ variables are in it). What it does next is
// set in <dir>/<dest>/:
//
//   - asleep: exit 255 (unreachable), as ssh does
//   - hold-<verb> (verb: machine or run): write held-<verb>, then wait
//     until release-<verb> exists before going on, so a test can act
//     between a dispatcher's read and its command
//   - report.json: printed for "tuckerbox machine ..." commands
//   - tuckerbox: run the real tuckerbox at this path with the remote
//     command's arguments, in the folder named by cwd, with each line of
//     env (KEY=VALUE) set, so the destination is another machine with its
//     own state directories
//   - otherwise: print out, exit with the number in exit (default 0)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	dir := filepath.Join(filepath.Dir(os.Getenv("XDG_STATE_HOME")), "fakessh")
	args := os.Args[1:]
	if f, err := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		data, _ := json.Marshal(args)
		fmt.Fprintf(f, "%s\n", data)
		f.Close()
	}
	if f, err := os.OpenFile(filepath.Join(dir, "env.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "%s\n", strings.Join(os.Environ(), "\n"))
		f.Close()
	}
	dest, line := "", ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-o":
			i++
		case a == "--":
			line = strings.Join(args[i+1:], " ")
			i = len(args)
		case strings.HasPrefix(a, "-"):
		default:
			dest = a
		}
	}
	words := split(line)
	if len(words) == 0 || words[0] != "tuckerbox" {
		fmt.Fprintf(os.Stderr, "fakessh: not a tuckerbox command: %q\n", line)
		os.Exit(2)
	}
	words = words[1:]
	d := filepath.Join(dir, dest)
	if _, err := os.Stat(filepath.Join(d, "asleep")); err == nil {
		fmt.Fprintf(os.Stderr, "ssh: connect to host %s port 22: Operation timed out\n", dest)
		os.Exit(255)
	}
	if len(words) > 0 {
		verb := words[0]
		if _, err := os.Stat(filepath.Join(d, "hold-"+verb)); err == nil {
			os.WriteFile(filepath.Join(d, "held-"+verb), nil, 0o644)
			for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if _, err := os.Stat(filepath.Join(d, "release-"+verb)); err == nil {
					break
				}
			}
		}
	}
	if len(words) > 0 && words[0] == "machine" {
		if data, err := os.ReadFile(filepath.Join(d, "report.json")); err == nil {
			os.Stdout.Write(data)
			return
		}
	}
	if exe, err := os.ReadFile(filepath.Join(d, "tuckerbox")); err == nil {
		cmd := exec.Command(strings.TrimSpace(string(exe)), words...)
		if cwd, err := os.ReadFile(filepath.Join(d, "cwd")); err == nil {
			cmd.Dir = strings.TrimSpace(string(cwd))
		}
		cmd.Env = os.Environ()
		if env, err := os.ReadFile(filepath.Join(d, "env")); err == nil {
			for _, kv := range strings.Split(string(env), "\n") {
				if kv = strings.TrimSpace(kv); kv != "" {
					cmd.Env = append(cmd.Env, kv)
				}
			}
		}
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if out, err := os.ReadFile(filepath.Join(d, "out")); err == nil {
		os.Stdout.Write(out)
	}
	if code, err := os.ReadFile(filepath.Join(d, "exit")); err == nil {
		n, _ := strconv.Atoi(strings.TrimSpace(string(code)))
		os.Exit(n)
	}
}

// split splits a POSIX shell command line of plain words and single-quoted
// strings ('\” for a quote), as tuckerbox's runner writes it.
func split(s string) []string {
	var out []string
	var cur strings.Builder
	in, word := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case in && c == '\'':
			in = false
		case in:
			cur.WriteByte(c)
		case c == '\'':
			in, word = true, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			word = true
		case c == ' ' || c == '\t':
			if word {
				out = append(out, cur.String())
				cur.Reset()
				word = false
			}
		default:
			cur.WriteByte(c)
			word = true
		}
	}
	if word {
		out = append(out, cur.String())
	}
	return out
}
