package edits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// ValidateVersion is the format version of tuckerbox validate --json.
// Fields may be added within a version, never renamed or removed.
const ValidateVersion = 1

// Result is the outcome of strict validation, as tuckerbox validate --json
// prints it.
type Result struct {
	Version int    `json:"version"`
	Change  string `json:"change"`
	// Folder is where the change was validated (see Place).
	Folder string `json:"folder"`
	Valid  bool   `json:"valid"`
	// Errors lists every issue strict validation reports; empty when valid.
	Errors []Issue `json:"errors"`
}

// Issue is one validation issue.
type Issue struct {
	// Level is openspec's level, such as ERROR or WARNING. Strict
	// validation fails on any of them.
	Level string `json:"level"`
	// Path is the file the issue is in, relative to the change, if known.
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// Validator runs openspec validate --strict.
type Validator struct {
	// Program is the openspec executable; "" means "openspec" on PATH.
	Program string
}

// Validate runs `openspec validate <change> --strict` in p.Dir and returns
// its result. The error is for openspec failing to run at all, not for an
// invalid change.
func (v Validator) Validate(ctx context.Context, p Place) (*Result, error) {
	prog := v.Program
	if prog == "" {
		prog = "openspec"
	}
	cmd := exec.CommandContext(ctx, prog, "validate", p.Change, "--type", "change", "--strict", "--json", "--no-interactive")
	cmd.Dir = p.Dir
	cmd.Env = append(cmd.Environ(), "NO_COLOR=1", "OPENSPEC_TELEMETRY=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, fmt.Errorf("running openspec validate %s: %w", p.Change, err)
		}
	}
	res := parseValidate(p.Change, stdout.Bytes(), stderr.Bytes(), err == nil)
	res.Folder = p.Dir
	return res, nil
}

// parseValidate reads openspec validate --json output. Output that isn't
// that JSON (an older openspec) is judged by the exit status, and its text
// becomes the one issue.
func parseValidate(change string, stdout, stderr []byte, exitOK bool) *Result {
	res := &Result{Version: ValidateVersion, Change: change, Errors: []Issue{}}
	var doc struct {
		Items []struct {
			ID     string  `json:"id"`
			Valid  bool    `json:"valid"`
			Issues []Issue `json:"issues"`
		} `json:"items"`
		Status []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"status"`
	}
	if json.Unmarshal(stdout, &doc) == nil && (len(doc.Items) > 0 || len(doc.Status) > 0) {
		res.Valid = exitOK && len(doc.Status) == 0
		for _, it := range doc.Items {
			if !it.Valid {
				res.Valid = false
			}
			res.Errors = append(res.Errors, it.Issues...)
		}
		for _, s := range doc.Status {
			res.Errors = append(res.Errors, Issue{Level: strings.ToUpper(s.Severity), Message: s.Message})
		}
		if res.Valid {
			res.Errors = []Issue{}
		} else if len(res.Errors) == 0 {
			res.Errors = append(res.Errors, Issue{Level: "ERROR", Message: "openspec validate failed without naming an issue"})
		}
		return res
	}
	res.Valid = exitOK
	if !exitOK {
		msg := strings.TrimSpace(strings.TrimSpace(string(stdout)) + "\n" + strings.TrimSpace(string(stderr)))
		if msg == "" {
			msg = "openspec validate failed"
		}
		res.Errors = append(res.Errors, Issue{Level: "ERROR", Message: msg})
	}
	return res
}

// Text is the result for people.
func (r *Result) Text() string {
	if r.Valid {
		return fmt.Sprintf("%s is valid (openspec validate --strict) in %s\n", r.Change, r.Folder)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s is not valid (openspec validate --strict) in %s:\n", r.Change, r.Folder)
	for _, e := range r.Errors {
		b.WriteString("  ✗ [" + e.Level + "] ")
		if e.Path != "" {
			b.WriteString(e.Path + ": ")
		}
		b.WriteString(strings.ReplaceAll(e.Message, "\n", "\n    ") + "\n")
	}
	return b.String()
}
