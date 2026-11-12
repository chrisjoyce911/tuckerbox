package runs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// OpenSpec runs the openspec CLI in a repository. Tuckerbox asks it which
// changes exist and whether they are valid, and parses no spec files itself.
type OpenSpec struct {
	// Dir is the checkout to run in.
	Dir string
	// Program is the openspec executable; "" means "openspec" on PATH.
	Program string
}

func (o OpenSpec) cmd(ctx context.Context, args ...string) *exec.Cmd {
	prog := o.Program
	if prog == "" {
		prog = "openspec"
	}
	c := exec.CommandContext(ctx, prog, args...)
	c.Dir = o.Dir
	c.Env = append(c.Environ(), "NO_COLOR=1", "OPENSPEC_TELEMETRY=0")
	return c
}

// List returns the ids of the active (not archived) changes, sorted.
func (o OpenSpec) List(ctx context.Context) ([]string, error) {
	c := o.cmd(ctx, "list", "--json")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("openspec list --json: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var doc struct {
		Changes []struct {
			Name string `json:"name"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("openspec list --json: unexpected output: %v", err)
	}
	var ids []string
	for _, c := range doc.Changes {
		if c.Name != "" {
			ids = append(ids, c.Name)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ValidationError is a change that openspec validate rejects.
type ValidationError struct {
	Change string
	Output string
}

func (e *ValidationError) Error() string {
	msg := fmt.Sprintf("openspec validate %s failed", e.Change)
	if e.Output != "" {
		msg += ":\n" + e.Output
	}
	return msg
}

// Validate runs openspec validate on change and returns a *ValidationError
// with its output if the change is not valid.
func (o OpenSpec) Validate(ctx context.Context, change string) error {
	c := o.cmd(ctx, "validate", change, "--type", "change", "--no-interactive")
	out, err := c.CombinedOutput()
	if err == nil {
		return nil
	}
	if _, ok := err.(*exec.ExitError); !ok {
		return fmt.Errorf("running openspec validate %s: %w", change, err)
	}
	return &ValidationError{Change: change, Output: strings.TrimSpace(string(out))}
}
