package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
)

// HistoryVersion is the format version of tuckerbox history --json. Like
// tuckerbox status --json, fields may be added within a version, never
// renamed or removed.
const HistoryVersion = 1

// noRunError means a change has no run on this machine.
type noRunError struct{ change string }

func (e *noRunError) Error() string {
	return fmt.Sprintf("no run of change %s on this machine", e.change)
}

// historyEvent is one event in tuckerbox history --json: a history.Event
// with its time as RFC 3339 in the machine's local time zone. See package
// history for what each kind carries.
type historyEvent struct {
	Time     string          `json:"time"`
	Kind     history.Kind    `json:"kind"`
	Step     string          `json:"step"`
	Round    int             `json:"round"`
	Agent    string          `json:"agent"`
	Machine  string          `json:"machine"`
	To       string          `json:"to,omitempty"`
	Status   agent.Status    `json:"status,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Findings []agent.Finding `json:"findings,omitempty"`
	ExitCode *int            `json:"exit_code,omitempty"`
	Reason   string          `json:"reason,omitempty"`
}

// history: tuckerbox history <change> [--json]
func (a App) history(args []string) int {
	var asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("history", "usage: tuckerbox history <change> [--json]")
	}
	change := pos[0]
	var evs []history.Event
	l, err := a.locate(change)
	var nr *noRunError
	switch {
	case errors.As(err, &nr):
		// Never run: an empty history.
	case err != nil:
		return a.errf("history", "%v", err)
	default:
		evs, err = history.Read(history.File(l.State))
		if err != nil {
			return a.errf("history", "%v", err)
		}
	}
	if asJSON {
		out := []historyEvent{}
		for _, e := range evs {
			out = append(out, historyEvent{
				Time: rfc3339(e.Time), Kind: e.Kind, Step: e.Step, Round: e.Round, Agent: e.Agent,
				Machine: e.Machine, To: e.To, Status: e.Status, Summary: e.Summary,
				Findings: e.Findings, ExitCode: e.ExitCode, Reason: e.Reason,
			})
		}
		return a.writeJSON(map[string]any{"version": HistoryVersion, "change": change, "events": out})
	}
	if len(evs) == 0 {
		fmt.Fprintf(a.Stdout, "no history for %s\n", change)
		return 0
	}
	rows := [][]string{{"TIME", "EVENT", "STEP", "ROUND", "AGENT", "DETAIL"}}
	var findings [][]agent.Finding
	for _, e := range evs {
		rows = append(rows, []string{
			e.Time.Local().Format("2006-01-02 15:04:05 MST"), string(e.Kind), dash(e.Step),
			strconv.Itoa(e.Round), dash(e.Agent), historyDetail(e),
		})
		findings = append(findings, e.Findings)
	}
	// Findings go under their event, so they are printed after the table
	// is laid out.
	var sb strings.Builder
	writeTable(&sb, rows)
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	fmt.Fprintln(a.Stdout, lines[0])
	for i, line := range lines[1:] {
		fmt.Fprintln(a.Stdout, line)
		if evs[i].Kind == history.StepFinished {
			continue // the rejected event lists them
		}
		for _, f := range findings[i] {
			where := ""
			if f.File != "" {
				where = " " + f.File + ":"
			}
			fmt.Fprintf(a.Stdout, "    - [%s]%s %s\n", f.Severity, where, oneLine(f.Detail))
		}
	}
	return 0
}

// historyDetail is the DETAIL column: where the run went next, the result
// and summary, or the reason.
func historyDetail(e history.Event) string {
	var parts []string
	switch e.Kind {
	case history.StepFinished:
		s := string(e.Status)
		if e.ExitCode != nil && *e.ExitCode != 0 {
			s += fmt.Sprintf(" (exit %d)", *e.ExitCode)
		}
		parts = append(parts, s)
	case history.Handoff, history.Rejected:
		if e.To != "" {
			parts = append(parts, "-> "+e.To)
		}
	}
	if e.Kind != history.StepFinished && e.Summary != "" {
		parts = append(parts, oneLine(e.Summary))
	}
	if e.Reason != "" {
		parts = append(parts, oneLine(e.Reason))
	}
	if len(e.Findings) > 0 && e.Kind == history.StepFinished {
		parts = append(parts, fmt.Sprintf("%d findings", len(e.Findings)))
	}
	return strings.Join(parts, ": ")
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// oneLine joins s onto one line and shortens it for the table.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
