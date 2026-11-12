package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
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
	Scope    string          `json:"scope,omitempty"`
	OldScope string          `json:"old_scope,omitempty"`
}

// history: tuckerbox history <change> [--json]
func (a App) history(args []string) int {
	var asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("history", "usage: tuckerbox history <change> [--json]")
	}
	change := pos[0]
	l, err := a.locateHistory(change)
	var nr *noRunError
	var evs []history.Event
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
				Scope: e.Scope, OldScope: e.OldScope,
			})
		}
		return a.writeJSON(map[string]any{"version": HistoryVersion, "change": change, "events": out})
	}
	if len(evs) == 0 {
		fmt.Fprintf(a.Stdout, "no history for %s\n", change)
		return 0
	}
	a.writeHistory(evs)
	return 0
}

// locateHistory finds change's run for tuckerbox history. Inside a
// repository only its own worktrees count, so a change that never ran
// there has no history even if another repository on this machine ran a
// change with the same id. Outside a repository the machine index is used,
// as for the other run commands.
func (a App) locateHistory(change string) (located, error) {
	ctx := context.Background()
	if err := statedir.CheckID("change", change); err != nil {
		return located{}, err
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.locate(change)
	}
	repo, err := runs.OpenRepo(ctx, wd)
	if err != nil {
		return a.locate(change)
	}
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		return located{}, err
	}
	for _, wt := range wts {
		st, err := statedir.Resolve(ctx, wt)
		if err != nil {
			continue
		}
		if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
			return located{State: st, Run: r}, nil
		}
	}
	return located{}, &noRunError{change}
}

// writeHistory prints evs as a table, oldest first, in local time. A
// step's result row carries its summary and findings; when a rejection
// follows it, the findings are listed under the rejection instead, once.
func (a App) writeHistory(evs []history.Event) {
	rows := [][]string{{"TIME", "EVENT", "STEP", "ROUND", "AGENT", "DETAIL"}}
	findings := make([][]agent.Finding, len(evs))
	for i, e := range evs {
		var prev *history.Event
		if i > 0 {
			prev = &evs[i-1]
		}
		rows = append(rows, []string{
			e.Time.Local().Format("2006-01-02 15:04:05 MST"), string(e.Kind), dash(e.Step),
			strconv.Itoa(e.Round), dash(e.Agent), historyDetail(e, prev),
		})
		findings[i] = e.Findings
		if e.Kind == history.Rejected && sameResult(prev, e) && len(e.Findings) > 0 {
			findings[i-1] = nil
		}
	}
	// Findings go under their event, so they are printed after the table
	// is laid out.
	var sb strings.Builder
	writeTable(&sb, rows)
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	fmt.Fprintln(a.Stdout, lines[0])
	for i, line := range lines[1:] {
		fmt.Fprintln(a.Stdout, line)
		for _, f := range findings[i] {
			where := ""
			if f.File != "" {
				where = " " + f.File + ":"
			}
			fmt.Fprintf(a.Stdout, "    - [%s]%s %s\n", f.Severity, where, oneLine(f.Detail))
		}
	}
}

// sameResult reports whether prev is the step_finished event that e (a
// hand-off or rejection) follows from.
func sameResult(prev *history.Event, e history.Event) bool {
	return prev != nil && prev.Kind == history.StepFinished && prev.Step == e.Step && prev.Round == e.Round
}

// historyDetail is the DETAIL column: the result and its summary, where
// the run went next, or the reason. A hand-off or rejection leaves out the
// summary already shown on the result just before it.
func historyDetail(e history.Event, prev *history.Event) string {
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
	if e.Summary != "" && !(sameResult(prev, e) && prev.Summary == e.Summary) {
		parts = append(parts, oneLine(e.Summary))
	}
	if e.Reason != "" {
		parts = append(parts, oneLine(e.Reason))
	}
	switch {
	case e.Kind == history.ScopeStarted:
		parts = append(parts, "tasks "+allIfEmpty(e.OldScope)+" -> "+allIfEmpty(e.Scope))
	case e.Scope != "":
		parts = append(parts, "tasks "+e.Scope)
	}
	return strings.Join(parts, ": ")
}

func allIfEmpty(s string) string {
	if s == "" {
		return "all"
	}
	return s
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
