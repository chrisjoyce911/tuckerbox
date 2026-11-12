// Command fakehc stands in for herdr-core's hc in tests. It supports:
//
//	hc server status   succeeds, printing a status object, when
//	                   FAKEHC_SERVER is "up"; otherwise fails, saying the
//	                   server is not running
//	hc run --bg [--json] --name N --cwd D [--label k=v]... [--no-parent]
//	       [--no-notify] [--outlive-parent] -- argv...
//	                   starts argv in D as a detached "terminal" whose
//	                   output goes to a file and whose input comes from
//	                   another; refuses while a live terminal is called N
//	hc get [--json] N  the terminal as JSON, or a not_found error (exit 3)
//	hc kill [--json] N ends its program (hangup, then kill) and waits
//	hc read [--lines n] N
//	                   prints everything it wrote
//	hc send [--json] N TEXT
//	                   types TEXT and enter into it
//	hc ls --json       the live terminals
//	hc killall         ends every terminal (test cleanup)
//	hc status N STATE  sets the status the terminal's program reports, as
//	                   an agent's OSC 7501 report would ("working",
//	                   "blocked", ...; "" clears it). get shows it as hc
//	                   0.1 does, an object with its state, and status_ms
//	                   when it was set; get's output_ms is when the
//	                   terminal last wrote output.
//
// Terminals live in FAKEHC_DIR: <name>.json (the record), <name>.out and
// <name>.in. Each call is appended to FAKEHC_DIR/calls.log as one JSON
// array of its arguments. A lock file makes run's name check and start
// atomic, as herdr-core's server does. Terminals are Unix only.
//
// Faults, for tests, are files in FAKEHC_DIR read at each call:
//
//	kill-mode    "fail": kill fails without ending anything; "ignore": kill
//	             says it ended the terminal but leaves it live
//	run-delay    a Go duration: run waits that long after its terminal has
//	             started, before it answers
//	get-fail     a count: get answers that many more times, then fails
//	             with a server error ("0" fails every get)
//	run-reply    "garbage": run starts its terminal, then answers with
//	             text that is not JSON
//	launch-later a Go duration: run hands its start to a detached process
//	             that launches the terminal that long later, as a server
//	             still working on a request whose client went away would,
//	             and never answers itself (it waits to be killed)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// record is a terminal, in the shape hc get --json prints.
type record struct {
	Address string            `json:"address"`
	Name    string            `json:"name"`
	Cwd     string            `json:"cwd"`
	Argv    []string          `json:"argv"`
	Labels  map[string]string `json:"labels"`
	State   string            `json:"state"`
	Status  json.RawMessage   `json:"status"`
	Exit    *int              `json:"exit_code"`
	// StatusMs and OutputMs are filled in by get.
	StatusMs *int64 `json:"status_ms,omitempty"`
	OutputMs *int64 `json:"output_ms,omitempty"`
	Pid      int    `json:"pid"`
	Seq      int    `json:"seq"`
}

func dir() string {
	d := os.Getenv("FAKEHC_DIR")
	if d == "" {
		fail(1, "fakehc: FAKEHC_DIR is not set")
	}
	must(os.MkdirAll(d, 0o755))
	return d
}

func path(name, ext string) string { return filepath.Join(dir(), name+ext) }

func fail(code int, format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(code)
}

func must(err error) {
	if err != nil {
		fail(1, "fakehc: %v", err)
	}
}

// hcError prints an hc error object and exits with code.
func hcError(code int, kind, msg string) {
	data, _ := json.Marshal(map[string]any{"error": map[string]string{"kind": kind, "message": msg}})
	fmt.Println(string(data))
	os.Exit(code)
}

func notFound(name string) { hcError(3, "not_found", fmt.Sprintf("no terminal %q", name)) }

func readRecord(name string) (*record, error) {
	data, err := os.ReadFile(path(name, ".json"))
	if err != nil {
		return nil, err
	}
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	// A terminal whose host process is gone has finished, whatever its
	// record says (its host was killed).
	if r.State == "live" && !alive(r.Pid) {
		r.State = "finished"
	}
	return &r, nil
}

func writeRecord(r *record) {
	data, _ := json.Marshal(r)
	tmp := path(r.Name, ".json.tmp")
	must(os.WriteFile(tmp, data, 0o644))
	must(os.Rename(tmp, path(r.Name, ".json")))
}

func lock() func() {
	l := flock.New(filepath.Join(dir(), ".lock"))
	must(l.Lock())
	return func() { _ = l.Unlock() }
}

func logCall(args []string) {
	if os.Getenv("FAKEHC_DIR") == "" {
		return
	}
	data, _ := json.Marshal(args)
	f, err := os.OpenFile(filepath.Join(dir(), "calls.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		fmt.Fprintln(f, string(data))
		f.Close()
	}
}

func main() {
	args := os.Args[1:]
	// A launch-later start was logged when its call came.
	if len(args) > 0 && args[0] != "host" && os.Getenv("FAKEHC_LATER") == "" {
		logCall(args)
	}
	if strings.Join(args, " ") == "server status" {
		if os.Getenv("FAKEHC_SERVER") == "up" {
			fmt.Println(`{"core":{"name":"fake"},"live":0}`)
			return
		}
		fmt.Fprintln(os.Stderr, "herdr-core server is not running")
		os.Exit(1)
	}
	if len(args) == 0 {
		fail(2, "fakehc: no command")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "run":
		run(rest)
	case "host":
		host(rest[0])
	case "get":
		name := lastPositional(rest)
		getFault()
		r, err := readRecord(name)
		if err != nil {
			notFound(name)
		}
		if fi, err := os.Stat(path(name, ".status")); err == nil {
			state, _ := os.ReadFile(path(name, ".status"))
			ms := fi.ModTime().UnixMilli()
			r.StatusMs = &ms
			r.Status = json.RawMessage("null")
			if st := strings.TrimSpace(string(state)); st != "" {
				r.Status, _ = json.Marshal(map[string]string{"app": "fake", "msg": "", "state": st})
			}
		}
		if fi, err := os.Stat(path(name, ".out")); err == nil {
			ms := fi.ModTime().UnixMilli()
			r.OutputMs = &ms
		}
		data, _ := json.Marshal(r)
		fmt.Println(string(data))
	case "status":
		pos := positionals(rest)
		if len(pos) != 2 {
			fail(2, "fakehc status: want <terminal> <state>")
		}
		if _, err := readRecord(pos[0]); err != nil {
			notFound(pos[0])
		}
		must(os.WriteFile(path(pos[0], ".status"), []byte(pos[1]), 0o644))
	case "kill":
		kill(lastPositional(rest))
	case "read":
		name := lastPositional(rest)
		if _, err := readRecord(name); err != nil {
			notFound(name)
		}
		data, _ := os.ReadFile(path(name, ".out"))
		os.Stdout.Write(data)
	case "send":
		pos := positionals(rest)
		if len(pos) != 2 {
			fail(2, "fakehc send: want <terminal> <text>")
		}
		r, err := readRecord(pos[0])
		if err != nil || r.State != "live" {
			notFound(pos[0])
		}
		f, err := os.OpenFile(path(pos[0], ".in"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		must(err)
		fmt.Fprintln(f, pos[1])
		f.Close()
		fmt.Println(`{"sent":true}`)
	case "ls":
		entries, _ := os.ReadDir(dir())
		var live []*record
		for _, e := range entries {
			if n, ok := strings.CutSuffix(e.Name(), ".json"); ok {
				if r, err := readRecord(n); err == nil && r.State == "live" {
					live = append(live, r)
				}
			}
		}
		data, _ := json.Marshal(map[string]any{"terminals": live})
		fmt.Println(string(data))
	case "killall":
		entries, _ := os.ReadDir(dir())
		for _, e := range entries {
			if n, ok := strings.CutSuffix(e.Name(), ".json"); ok {
				if r, err := readRecord(n); err == nil && r.State == "live" {
					end(r)
				}
			}
		}
	default:
		fail(2, "fakehc: unsupported command %q", strings.Join(args, " "))
	}
}

// positionals drops flags (and --lines's value) from args.
func positionals(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--lines":
			i++
		case strings.HasPrefix(args[i], "--"):
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func lastPositional(args []string) string {
	pos := positionals(args)
	if len(pos) == 0 {
		fail(2, "fakehc: which terminal?")
	}
	return pos[len(pos)-1]
}

func run(args []string) {
	r := &record{Labels: map[string]string{}, State: "live"}
	bg := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 >= len(args) {
				fail(2, "fakehc run: %s needs a value", a)
			}
			i++
			return args[i]
		}
		switch a {
		case "--bg":
			bg = true
		case "--json", "--no-parent", "--no-notify", "--outlive-parent":
		case "--name":
			r.Name = next()
		case "--cwd":
			r.Cwd = next()
		case "--label":
			k, v, _ := strings.Cut(next(), "=")
			r.Labels[k] = v
		case "--":
			r.Argv = args[i+1:]
			i = len(args)
		default:
			fail(2, "fakehc run: unsupported argument %q", a)
		}
	}
	if !bg || r.Name == "" || r.Cwd == "" || len(r.Argv) == 0 {
		fail(2, "fakehc run: want --bg --name --cwd and a program")
	}
	if d, err := time.ParseDuration(fault("launch-later")); err == nil && os.Getenv("FAKEHC_LATER") == "" {
		must(launchLater(d, args))
		time.Sleep(time.Hour)
		return
	}
	if d, err := time.ParseDuration(os.Getenv("FAKEHC_LATER")); err == nil {
		time.Sleep(d)
	}
	unlock := lock()
	defer unlock()
	if old, err := readRecord(r.Name); err == nil {
		if old.State == "live" {
			hcError(1, "name_taken", fmt.Sprintf("a live terminal is called %q", r.Name))
		}
		r.Seq = old.Seq + 1
	}
	r.Address = fmt.Sprintf("t%d%s", r.Seq, r.Name[len(r.Name)-2:])
	_ = os.Remove(path(r.Name, ".out"))
	must(os.WriteFile(path(r.Name, ".in"), nil, 0o644))
	pid, err := startHost(r.Name)
	must(err)
	r.Pid = pid
	writeRecord(r)
	if d, err := time.ParseDuration(fault("run-delay")); err == nil {
		time.Sleep(d)
	}
	if fault("run-reply") == "garbage" {
		fmt.Println("started, probably")
		return
	}
	data, _ := json.Marshal(map[string]any{"address": r.Address, "name": r.Name, "cwd": r.Cwd, "state": "live"})
	fmt.Println(string(data))
}

// getFault counts down the get-fail fault, failing once it is spent.
func getFault() {
	if fault("get-fail") == "" {
		// No fault: no lock, which run holds through its run-delay.
		return
	}
	unlock := lock()
	defer unlock()
	n, err := strconv.Atoi(fault("get-fail"))
	if err != nil {
		return
	}
	if n <= 0 {
		unlock()
		hcError(1, "server", "fake get failure")
	}
	must(os.WriteFile(filepath.Join(dir(), "get-fail"), []byte(strconv.Itoa(n-1)), 0o644))
}

// fault returns the test fault file's content, or "".
func fault(name string) string {
	data, _ := os.ReadFile(filepath.Join(dir(), name))
	return strings.TrimSpace(string(data))
}

func kill(name string) {
	r, err := readRecord(name)
	if err != nil {
		notFound(name)
	}
	switch fault("kill-mode") {
	case "fail":
		hcError(1, "server", "fake kill failure")
	case "ignore":
		fmt.Printf(`{"killed":%q}`+"\n", r.Address)
		return
	}
	if r.State == "live" {
		end(r)
	}
	fmt.Printf(`{"killed":%q}`+"\n", r.Address)
}

// end hangs up the terminal's process group, kills it if it lingers, and
// waits until its host is gone.
func end(r *record) {
	hangup(r.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for alive(r.Pid) {
		if time.Now().After(deadline) {
			killGroup(r.Pid)
			deadline = time.Now().Add(time.Hour)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
