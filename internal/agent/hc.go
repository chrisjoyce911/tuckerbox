package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
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
	Status  *string           `json:"status"`
	Labels  map[string]string `json:"labels"`
	Exit    *int              `json:"exit_code"`
}

// Live reports whether the terminal's program is still running.
func (t *HCTerminal) Live() bool { return t != nil && t.State == "live" }

// Waiting reports whether herdr-core sees the agent in the terminal
// waiting for input.
func (t *HCTerminal) Waiting() bool {
	return t != nil && t.Status != nil && *t.Status == "waiting"
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
func (h HC) End(ctx context.Context, name string, timeout time.Duration) error {
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

// Send pastes text into the terminal and presses enter.
func (h HC) Send(ctx context.Context, name, text string) error {
	_, err := h.run(ctx, "send", "--json", name, text)
	return err
}
