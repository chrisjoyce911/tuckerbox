package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// Status is a step's reported outcome.
type Status string

// The statuses an agent may report.
const (
	Done     Status = "done"
	Rejected Status = "rejected"
	Blocked  Status = "blocked"
	Failed   Status = "failed"
)

// Severities a finding may have.
var severities = []string{"critical", "high", "medium", "low", "info"}

// Finding is one problem an agent reports.
type Finding struct {
	Severity string `json:"severity"`
	// File is the path the finding is about, relative to the worktree. It may
	// be empty for a finding about the change as a whole.
	File   string `json:"file"`
	Detail string `json:"detail"`
}

// Result is the JSON an agent writes to its result file.
//
// Change, Step and Round repeat what the brief told the agent, so a file
// left by an earlier round or another step is recognised and never accepted
// (formal/runs.tla, invariant StaleNeverAccepted).
type Result struct {
	Change   string    `json:"change"`
	Step     string    `json:"step"`
	Round    int       `json:"round"`
	Status   Status    `json:"status"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings,omitempty"`
}

// Expect is the step and round the engine is waiting for.
type Expect struct {
	Change string
	Step   string
	Round  int
}

// ErrNoResult means the result file does not exist (yet).
var ErrNoResult = errors.New("no result file")

// StaleError is a well-formed result for a different step or round. The
// engine ignores it and keeps waiting.
type StaleError struct {
	Path string
	Got  Expect
	Want Expect
}

func (e *StaleError) Error() string {
	switch {
	case e.Got.Change != e.Want.Change:
		return fmt.Sprintf("%s: result is for change %q, want %q", e.Path, e.Got.Change, e.Want.Change)
	case e.Got.Step != e.Want.Step:
		return fmt.Sprintf("%s: result is for step %q, want %q", e.Path, e.Got.Step, e.Want.Step)
	case e.Got.Round < e.Want.Round:
		return fmt.Sprintf("%s: stale result from round %d, the step is on round %d", e.Path, e.Got.Round, e.Want.Round)
	default:
		return fmt.Sprintf("%s: result names round %d, the step is on round %d", e.Path, e.Got.Round, e.Want.Round)
	}
}

// MalformedError is a result file that is not valid JSON or breaks the
// contract. It may be a file the agent is still writing.
type MalformedError struct {
	Path   string
	Reason string
}

func (e *MalformedError) Error() string {
	return fmt.Sprintf("%s: malformed result: %s", e.Path, e.Reason)
}

// IsIgnored reports whether err is a stale or malformed result, which the
// engine logs and ignores.
func IsIgnored(err error) bool {
	var s *StaleError
	var m *MalformedError
	return errors.As(err, &s) || errors.As(err, &m)
}

// maxResultSize bounds how much of a result file is read.
const maxResultSize = 1 << 20

// ReadResult reads the result file at path and accepts it only if it is well
// formed and names exactly the change, step and round in want. It returns
// ErrNoResult, a *StaleError or a *MalformedError otherwise.
func ReadResult(path string, want Expect) (Result, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Result{}, ErrNoResult
	}
	if err != nil {
		return Result{}, err
	}
	return ParseResult(path, data, want)
}

// ParseResult checks data as the result file at path (used in messages).
func ParseResult(path string, data []byte, want Expect) (Result, error) {
	if len(data) > maxResultSize {
		return Result{}, &MalformedError{path, fmt.Sprintf("larger than %d bytes", maxResultSize)}
	}
	if strings.TrimSpace(string(data)) == "" {
		return Result{}, &MalformedError{path, "file is empty"}
	}
	// Decode into raw fields first so a missing round is told apart from 0.
	var raw struct {
		Change   *string         `json:"change"`
		Step     *string         `json:"step"`
		Round    *int            `json:"round"`
		Status   *Status         `json:"status"`
		Summary  *string         `json:"summary"`
		Findings json.RawMessage `json:"findings"`
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&raw); err != nil {
		return Result{}, &MalformedError{path, "not valid JSON: " + err.Error()}
	}
	if dec.More() {
		return Result{}, &MalformedError{path, "more than one JSON value"}
	}
	switch {
	case raw.Change == nil:
		return Result{}, &MalformedError{path, `missing "change"`}
	case raw.Step == nil:
		return Result{}, &MalformedError{path, `missing "step"`}
	case raw.Round == nil:
		return Result{}, &MalformedError{path, `missing "round"`}
	case raw.Status == nil:
		return Result{}, &MalformedError{path, `missing "status"`}
	}
	r := Result{Change: *raw.Change, Step: *raw.Step, Round: *raw.Round, Status: *raw.Status}
	if raw.Summary != nil {
		r.Summary = *raw.Summary
	}
	got := Expect{r.Change, r.Step, r.Round}
	if got != want {
		return Result{}, &StaleError{Path: path, Got: got, Want: want}
	}
	switch r.Status {
	case Done, Rejected, Blocked, Failed:
	default:
		return Result{}, &MalformedError{path, fmt.Sprintf("status %q is not one of done, rejected, blocked, failed", r.Status)}
	}
	if strings.TrimSpace(r.Summary) == "" {
		return Result{}, &MalformedError{path, `missing "summary"`}
	}
	if len(raw.Findings) > 0 && string(raw.Findings) != "null" {
		if err := json.Unmarshal(raw.Findings, &r.Findings); err != nil {
			return Result{}, &MalformedError{path, `"findings" must be a list of {severity, file, detail}: ` + err.Error()}
		}
	}
	for i := range r.Findings {
		f := &r.Findings[i]
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		if !validSeverity(f.Severity) {
			return Result{}, &MalformedError{path, fmt.Sprintf("finding %d: severity %q is not one of %s",
				i+1, f.Severity, strings.Join(severities, ", "))}
		}
		if strings.TrimSpace(f.Detail) == "" {
			return Result{}, &MalformedError{path, fmt.Sprintf("finding %d: missing \"detail\"", i+1)}
		}
	}
	return r, nil
}

func validSeverity(s string) bool {
	for _, v := range severities {
		if s == v {
			return true
		}
	}
	return false
}
