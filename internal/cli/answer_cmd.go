package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

const answerUsage = "usage: tuckerbox answer <change> [--resume] (<text> | --file <path> | -)"

// answer: tuckerbox answer <change> [--resume] (<text> | --file <path> | -)
//
// A blocked run's answer is recorded in run.json and history under the
// change lock, for the brief of the step when the run resumes; --resume
// then resumes it as tuckerbox resume does. A terminal agent that waits
// for the person gets the text typed into its terminal (hc send) instead,
// and the run keeps running (blocked-answers).
func (a App) answer(args []string) int {
	var resume bool
	var file string
	pos, err := parseArgs(args, map[string]*bool{"resume": &resume}, map[string]*string{"file": &file})
	if err != nil {
		return a.usageErr("answer", err.Error())
	}
	if len(pos) < 1 || len(pos) > 2 || (len(pos) == 2) == (file != "") {
		return a.usageErr("answer", answerUsage)
	}
	change := pos[0]
	var raw []byte
	switch {
	case file != "":
		raw, err = os.ReadFile(file)
	case pos[1] == "-":
		in := a.Stdin
		if in == nil {
			in = os.Stdin
		}
		// One byte more than the limit is enough to know it is too long.
		raw, err = io.ReadAll(io.LimitReader(in, 4*runs.MaxAnswer+1))
	default:
		raw = []byte(pos[1])
	}
	if err != nil {
		return a.errf("answer", "reading the answer: %v", err)
	}
	text, err := runs.CheckAnswer(string(raw))
	if err != nil {
		return a.errf("answer", "%v; nothing recorded", err)
	}
	l, err := a.locate(change)
	if err != nil {
		return a.errf("answer", "%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	by := gitUserName(ctx, l.Run.Worktree)
	if by == "" {
		by = gitUserName(ctx, l.Run.Repo)
	}
	if by == "" {
		return a.errf("answer", "git user.name is not set in %s; set it (git config user.name \"Your Name\") so the answer says who gave it; nothing recorded", l.Run.Repo)
	}
	ans := runs.Answer{Text: text, By: by, At: time.Now()}

	if l.active() {
		// A run process (or an edit) holds the change lock. Only a waiting
		// terminal agent can be answered now: its run keeps the lock.
		return a.answerLive(ctx, change, l, ans, resume)
	}
	hc, hcFound := a.hc()
	r, typed, err := runs.RecordAnswer(l.State, ans, func(rs *runs.Run) (string, error) {
		// The run process died while its agent works on in a terminal:
		// answering it needs no run process (we hold the lock).
		if resume {
			return "", errors.New("--resume: the run's agent works on in a terminal (not watched); answer without --resume, or run tuckerbox resume " + change + " to watch it again")
		}
		if !hcFound {
			return "", nil
		}
		return a.sendWaiting(ctx, hc, rs, text)
	})
	var nb *runs.NotBlockedError
	switch {
	case errors.As(err, &nb):
		return a.errf("answer", "%v", err)
	case errors.Is(err, runs.ErrLocked):
		return a.errf("answer", "%v", runs.LockedError(change, l.Run.Worktree, l.State))
	case err != nil:
		return a.errf("answer", "%v", err)
	}
	if typed != "" {
		fmt.Fprintf(a.Stdout, "%s: typed your answer into the %s agent's terminal %s; the run keeps running\n", change, r.Step, typed)
		return 0
	}
	fmt.Fprintf(a.Stdout, "%s: recorded your answer to step %s round %d (%d answer(s) for its next brief); the run stays blocked\n",
		change, r.Step, r.Round, countAnswers(r))
	if !resume {
		fmt.Fprintf(a.Stdout, "resume with tuckerbox resume %s, or tuckerbox answer %s --resume next time\n", change, change)
		return 0
	}
	// Resume as tuckerbox resume does, with its gates and refusals. The
	// answer stays recorded whatever the resume says.
	if code := a.restart("resume", false, change, runs.Request{Change: change, Mode: runs.Resume}); code != 0 {
		fmt.Fprintf(a.Stderr, "tuckerbox answer: %s: the answer is recorded, but the run was not resumed (see above); it stays blocked, and tuckerbox resume %s uses the answer\n", change, change)
		return code
	}
	return 0
}

// countAnswers counts the answers r's next brief holds.
func countAnswers(r *runs.Run) int {
	n := 0
	for _, g := range r.PendingAnswers(r.Step) {
		n += len(g.Answers)
	}
	return n
}

// answerLive answers a run whose change lock a process holds: when its
// step's terminal agent waits for the person, the text is typed into the
// terminal, and the answered event goes to the inbox for the run process
// to record.
func (a App) answerLive(ctx context.Context, change string, l located, ans runs.Answer, resume bool) int {
	r, err := runs.ReadRun(l.State.RunFile())
	if err != nil {
		return a.errf("answer", "%v", err)
	}
	if r.State == runs.Blocked {
		// Blocked, yet the lock is held: an edit (or a resume starting).
		return a.errf("answer", "%v; nothing recorded", runs.LockedError(change, r.Worktree, l.State))
	}
	hc, found := a.hc()
	if r.State != runs.Running || r.Terminal == nil || !found {
		return a.errf("answer", "%v", &runs.NotBlockedError{Change: change, State: r.State})
	}
	if resume {
		return a.errf("answer", "--resume: change %s is running; its agent gets the answer in its terminal: answer without --resume", change)
	}
	name, err := a.sendWaiting(ctx, hc, r, ans.Text)
	switch {
	case err != nil:
		return a.errf("answer", "%v; nothing recorded", err)
	case name == "":
		return a.errf("answer", "%v", &runs.NotBlockedError{Change: change, State: r.State})
	}
	ans.Step, ans.Round = r.Step, r.Round
	if err := runs.PostAnswer(l.State, runs.AnsweredEvent(change, ans, name)); err != nil {
		return a.errf("answer", "typed the answer into terminal %s, but couldn't record it in history: %v", name, err)
	}
	// The run process records it within a poll. One that has ended since
	// leaves it to whoever takes the lock next; take it now if it is free.
	if !l.active() {
		runs.DrainAnswerInboxUnlocked(l.State)
	}
	fmt.Fprintf(a.Stdout, "%s: typed your answer into the %s agent's terminal %s; the run keeps running\n", change, r.Step, name)
	return 0
}

// sendWaiting types text into rs's terminal, and presses enter, when its
// agent is live and waits for the person (run.json says so, or
// herdr-core's status does), and returns the terminal's name; "" when it
// doesn't wait, and nothing is typed.
func (a App) sendWaiting(ctx context.Context, hc agent.HC, rs *runs.Run, text string) (string, error) {
	if rs.Terminal == nil {
		return "", nil
	}
	name := rs.Terminal.Name
	t, err := hc.Get(ctx, name)
	var he *agent.HCError
	switch {
	case errors.As(err, &he) && he.NotFound():
		return "", nil
	case err != nil:
		return "", fmt.Errorf("asking herdr-core about terminal %s: %w", name, err)
	case !t.Live():
		return "", nil
	case rs.AgentState != agent.AgentWaiting && !t.Waiting():
		return "", nil
	}
	if err := hc.Send(ctx, name, text); err != nil {
		return "", fmt.Errorf("typing into terminal %s: %w", name, err)
	}
	return name, nil
}
