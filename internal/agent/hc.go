package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HC runs herdr-core's hc. The zero value runs "hc" from the PATH.
type HC struct {
	// Path is the hc program; "" means "hc".
	Path string
}

// HCTerminal is a herdr-core terminal, as hc get --json shows it.
type HCTerminal struct {
	Address string            `json:"address"`
	Name    string            `json:"name"`
	Cwd     string            `json:"cwd"`
	State   string            `json:"state"`
	Status  *HCStatus         `json:"status"`
	Labels  map[string]string `json:"labels"`
	Exit    *int              `json:"exit_code"`
	// CreatedMs, StatusMs and OutputMs are when herdr-core started the
	// terminal, last saw its status change and last saw it write output,
	// in Unix milliseconds; each may be absent.
	CreatedMs *int64 `json:"created_ms"`
	StatusMs  *int64 `json:"status_ms"`
	OutputMs  *int64 `json:"output_ms"`
}

// HCStatus is the status the terminal's program reports: hc ls prints it
// as a string ("working"), hc get as an object with its state, message
// and app.
type HCStatus struct {
	State string `json:"state"`
	Msg   string `json:"msg,omitempty"`
	App   string `json:"app,omitempty"`
}

// UnmarshalJSON reads a status as a string or as an object.
func (s *HCStatus) UnmarshalJSON(data []byte) error {
	var state string
	if json.Unmarshal(data, &state) == nil {
		*s = HCStatus{State: state}
		return nil
	}
	type plain HCStatus
	return json.Unmarshal(data, (*plain)(s))
}

// Working reports whether herdr-core sees the agent in the terminal at
// work.
func (t *HCTerminal) Working() bool {
	return t != nil && t.Status != nil && t.Status.State == "working"
}

// LastActivity is the last time herdr-core saw the terminal's program do
// something: its last output, else its last status change, else its
// start. It is zero when herdr-core reports none of them.
func (t *HCTerminal) LastActivity() time.Time {
	var ms int64
	if t != nil {
		// The first one herdr-core reports wins: a status change after the
		// last output doesn't replace the output's time.
		for _, v := range []*int64{t.OutputMs, t.StatusMs, t.CreatedMs} {
			if v != nil && *v > 0 {
				ms = *v
				break
			}
		}
	}
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// Live reports whether the terminal's program is still running.
func (t *HCTerminal) Live() bool { return t != nil && t.State == "live" }

// Waiting reports whether herdr-core sees the agent in the terminal
// waiting for the person: "blocked" (a permission prompt or a question),
// or "waiting".
func (t *HCTerminal) Waiting() bool {
	return t != nil && t.Status != nil && (t.Status.State == "blocked" || t.Status.State == "waiting")
}

// HCError is an error hc reported.
type HCError struct {
	Args    []string
	Kind    string
	Message string
	Err     error
}

func (e *HCError) Error() string {
	msg := e.Message
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("hc %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *HCError) Unwrap() error { return e.Err }

// NotFound reports whether hc said there is no such terminal.
func (e *HCError) NotFound() bool { return e.Kind == "not_found" }

func (h HC) path() string {
	if h.Path != "" {
		return h.Path
	}
	return HerdrCore
}

// run runs hc with args and returns its standard output.
func (h HC) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, h.path(), args...)
	cmd.WaitDelay = 2 * time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	he := &HCError{Args: args, Err: err}
	for _, text := range []string{string(out), stderr.String()} {
		var doc struct {
			Error *struct {
				Kind    string `json:"kind"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(text)), &doc) == nil && doc.Error != nil {
			he.Kind, he.Message = doc.Error.Kind, doc.Error.Message
			return out, he
		}
	}
	he.Message = strings.TrimSpace(oneLine(stderr.String() + " " + string(out)))
	if he.Message == "" {
		he.Message = err.Error()
	}
	return out, he
}

// Get returns the terminal called name, or nil when herdr-core has none.
func (h HC) Get(ctx context.Context, name string) (*HCTerminal, error) {
	out, err := h.run(ctx, "get", "--json", name)
	var he *HCError
	if errors.As(err, &he) && he.NotFound() {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t HCTerminal
	if err := json.Unmarshal(out, &t); err != nil {
		return nil, fmt.Errorf("hc get %s: %w", name, err)
	}
	return &t, nil
}

// HCStart is a terminal to start with hc run --bg.
type HCStart struct {
	Name   string
	Cwd    string
	Labels [][2]string
	Argv   []string
}

// Args is the hc command line for s.
func (s HCStart) Args() []string {
	args := []string{"run", "--bg", "--json", "--name", s.Name, "--cwd", s.Cwd, "--no-parent", "--no-notify", "--outlive-parent"}
	for _, l := range s.Labels {
		args = append(args, "--label", l[0]+"="+l[1])
	}
	return append(append(args, "--"), s.Argv...)
}

// Start starts a terminal in the background. herdr-core refuses it while
// a live terminal has the same name.
func (h HC) Start(ctx context.Context, s HCStart) (HCTerminal, error) {
	out, err := h.run(ctx, s.Args()...)
	if err != nil {
		return HCTerminal{}, err
	}
	var t HCTerminal
	if err := json.Unmarshal(out, &t); err != nil {
		return HCTerminal{}, fmt.Errorf("hc run: %w", err)
	}
	return t, nil
}

// Kill ends the terminal's program. A terminal that is gone already is
// not an error.
func (h HC) Kill(ctx context.Context, name string) error {
	_, err := h.run(ctx, "kill", "--json", name)
	var he *HCError
	if errors.As(err, &he) && he.NotFound() {
		return nil
	}
	return err
}

// End kills the terminal called name and waits, up to timeout, until it
// is no longer live.
//
// It first reads the terminal's text and keeps it for History: once
// herdr-core has ended Claude Code's full-screen interface nothing of it
// is left to read, so the step log would lose the agent's session.
func (h HC) End(ctx context.Context, name string, timeout time.Duration) error {
	h.snapshot(ctx, name)
	if err := h.Kill(ctx, name); err != nil {
		return err
	}
	return h.WaitGone(ctx, name, timeout)
}

// WaitGone waits, up to timeout, until no live terminal is called name.
func (h HC) WaitGone(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		t, err := h.Get(ctx, name)
		if err != nil {
			return err
		}
		if !t.Live() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("terminal %s is still live %s after it was ended", name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// historyLines is how many lines of history Read asks for.
const historyLines = "100000"

// Read returns the terminal's history and screen, as text.
func (h HC) Read(ctx context.Context, name string) ([]byte, error) {
	return h.run(ctx, "read", "--lines", historyLines, name)
}

// endSnapshots holds, by terminal name, the text End read of a terminal
// just before it ended it, until History takes it. Each End replaces its
// name's snapshot, so a later terminal of the same name never gets an
// older one's text.
var endSnapshots sync.Map

// snapshot keeps what the terminal called name shows now, for History.
func (h HC) snapshot(ctx context.Context, name string) {
	endSnapshots.Delete(name)
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := h.Read(rctx, name); err == nil && textLen(out) > 0 {
		endSnapshots.Store(name, out)
	}
}

// History returns the session of the terminal called name for the step
// log: what Read gives now, or what End read just before it ended the
// terminal when that holds more.
func (h HC) History(ctx context.Context, name string) ([]byte, error) {
	out, err := h.Read(ctx, name)
	if v, ok := endSnapshots.LoadAndDelete(name); ok {
		if snap := v.([]byte); err != nil || textLen(snap) > textLen(out) {
			return snap, nil
		}
	}
	return out, err
}

// textLen counts the text of a read that isn't space or hc's own notes
// ("[hc] incomplete: ...").
func textLen(out []byte) int {
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[hc] ") {
			continue
		}
		n += len(strings.Join(strings.Fields(line), ""))
	}
	return n
}

// screenLines is how many lines of history Screen asks for.
const screenLines = "60"

// Screen returns the terminal's screen and its last lines of history.
func (h HC) Screen(ctx context.Context, name string) ([]byte, error) {
	return h.run(ctx, "read", "--lines", screenLines, name)
}

// Keys presses the named keys ("down", "enter", ...) in the terminal.
func (h HC) Keys(ctx context.Context, name string, keys ...string) error {
	_, err := h.run(ctx, append([]string{"keys", "--json", name}, keys...)...)
	return err
}

// Send pastes text into the terminal and presses enter.
func (h HC) Send(ctx context.Context, name, text string) error {
	_, err := h.run(ctx, "send", "--json", name, text)
	return err
}
