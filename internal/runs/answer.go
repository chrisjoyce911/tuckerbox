package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// MaxAnswer is the largest answer tuckerbox answer takes, in bytes: it
// goes into briefs, which stay bounded.
const MaxAnswer = 16 << 10

// Answer is a person's answer to a blocked run's question
// (blocked-answers: Answering a blocked run).
type Answer struct {
	Text string    `json:"text"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	// Step and Round are the blocked step and round it answers.
	Step  string `json:"step"`
	Round int    `json:"round"`
	// Used is set once the step it answers has reported a result after
	// a brief that held it; later briefs leave it out.
	Used bool `json:"used,omitempty"`
}

// Question returns the question of a blocked run: the blocked result its
// step reported in its round, or nil when the run is not blocked by its
// step's result (a review limit, a failed scope check, shipping).
func (r *Run) Question() *Entry {
	if r.State != Blocked {
		return nil
	}
	return r.questionOf(r.Step, r.Round)
}

// questionOf returns the blocked result step reported in round, or nil.
func (r *Run) questionOf(step string, round int) *Entry {
	for i := len(r.History) - 1; i >= 0; i-- {
		e := &r.History[i]
		if e.Step == step && e.Round == round && e.Status == agent.Blocked {
			return e
		}
	}
	return nil
}

// PendingAnswers returns the questions step asked and the answers to them
// that no result of step has used yet, oldest first, for its brief.
func (r *Run) PendingAnswers(step string) []agent.Answered {
	var out []agent.Answered
	at := map[int]int{} // round -> index in out
	for _, a := range r.Answers {
		if a.Used || a.Step != step {
			continue
		}
		i, ok := at[a.Round]
		if !ok {
			q := agent.Result{Change: r.Change, Step: a.Step, Round: a.Round, Status: agent.Blocked}
			if e := r.questionOf(a.Step, a.Round); e != nil {
				q.Summary, q.Findings = e.Summary, e.Findings
			}
			out = append(out, agent.Answered{Question: q})
			i = len(out) - 1
			at[a.Round] = i
		}
		out[i].Answers = append(out[i].Answers, agent.PersonAnswer{Text: a.Text, By: a.By, At: a.At})
	}
	return out
}

// useAnswers marks step's answers used: the step reported a result.
func (r *Run) useAnswers(step string) {
	for i := range r.Answers {
		if r.Answers[i].Step == step {
			r.Answers[i].Used = true
		}
	}
}

// NotBlockedError means tuckerbox answer found a run that is neither
// blocked nor has a terminal agent waiting for the person.
type NotBlockedError struct {
	Change string
	State  State
}

func (e *NotBlockedError) Error() string {
	return fmt.Sprintf("change %s isn't blocked (it is %s), and no agent of it waits for you in a terminal; nothing recorded", e.Change, e.State)
}

// CheckAnswer trims an answer's text and refuses one that is empty or
// over MaxAnswer.
func CheckAnswer(text string) (string, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", errors.New("the answer is empty")
	case len(text) > MaxAnswer:
		return "", fmt.Errorf("the answer is %d bytes; the limit is %d (16 KiB)", len(text), MaxAnswer)
	}
	return text, nil
}

// AnsweredEvent is the answered history event for a.
func AnsweredEvent(change string, a Answer, terminal string) history.Event {
	return history.Event{Time: a.At, Kind: history.Answered, Change: change, Step: a.Step, Round: a.Round,
		Machine: history.Machine(), By: a.By, Text: a.Text, Terminal: terminal}
}

// The answer inbox: tuckerbox answer types into a waiting terminal agent's
// terminal while the run process holds the change lock, so it can't
// append to history itself (only the lock's holder writes history). It
// leaves the answered event in the inbox instead; the run process moves it
// into history within a poll, and whoever takes the lock next moves any
// event a dead run process left there.

// AnswerInbox is the inbox directory of a state directory.
func AnswerInbox(d statedir.Dir) string { return filepath.Join(string(d), "answers") }

// PostAnswer leaves ev in st's inbox, for the change lock's holder to add
// to history.
func PostAnswer(st statedir.Dir, ev history.Event) error {
	ev.V = history.Version
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	name := strconv.FormatInt(ev.Time.UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid()) + ".json"
	return writeAtomic(filepath.Join(AnswerInbox(st), name), append(data, '\n'))
}

// DrainAnswerInbox appends the events in st's inbox to history, oldest
// first, and removes them. The caller holds the change lock. An event is
// removed only after it is in history, so a crash between the two leaves
// it to be added again rather than lost.
func DrainAnswerInbox(st statedir.Dir) {
	dir := AnswerInbox(st)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})
	for _, n := range names {
		p := filepath.Join(dir, n)
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var ev history.Event
		if err != nil || json.Unmarshal(data, &ev) != nil || ev.Kind != history.Answered {
			// Not an answer (or unreadable): drop it rather than stall the
			// inbox on it.
			_ = os.Remove(p)
			continue
		}
		if history.Append(history.File(st), ev) == nil {
			_ = os.Remove(p)
		}
	}
}

// QuestionView is a run's open question as status and the board show it
// (blocked-answers: The question of a blocked run).
type QuestionView struct {
	// Kind is "blocked" for a blocked result's question, or "waiting" for
	// a terminal agent that waits for the person; Summary is then the
	// agent's last message, when its turn ended with one.
	Kind     string          `json:"kind"`
	Step     string          `json:"step"`
	Round    int             `json:"round"`
	Summary  string          `json:"summary"`
	Findings []agent.Finding `json:"findings"`
	// Answers are the answers given to it so far, oldest first.
	Answers []Answer `json:"answers"`
}

// Question kinds.
const (
	QuestionBlocked = "blocked"
	QuestionWaiting = "waiting"
)

// maxWaitingMessage bounds the last message shown as a waiting agent's
// question.
const maxWaitingMessage = 4 << 10

// Ask returns the question v (a run as people see it) puts to the person,
// or nil: the blocked result of a blocked run, or, for a running step
// whose terminal agent waits for the person (agentState), the agent's
// last message in st. Answers holds the answers given to a blocked
// question.
func Ask(v *Run, st statedir.Dir, agentState string) *QuestionView {
	if e := v.Question(); e != nil {
		q := &QuestionView{Kind: QuestionBlocked, Step: e.Step, Round: e.Round, Summary: e.Summary,
			Findings: e.Findings, Answers: []Answer{}}
		if q.Findings == nil {
			q.Findings = []agent.Finding{}
		}
		for _, a := range v.Answers {
			if a.Step == e.Step && a.Round == e.Round {
				q.Answers = append(q.Answers, a)
			}
		}
		return q
	}
	if v.State != Running || v.Terminal == nil || agentState != agent.AgentWaiting {
		return nil
	}
	q := &QuestionView{Kind: QuestionWaiting, Step: v.Step, Round: v.Round, Findings: []agent.Finding{}, Answers: []Answer{}}
	if st != "" {
		if data, err := os.ReadFile(st.LastMessage(v.Step, v.Round)); err == nil {
			msg := strings.TrimSpace(string(data))
			if len(msg) > maxWaitingMessage {
				cut := len(msg) - maxWaitingMessage
				for cut < len(msg) && !utf8.RuneStart(msg[cut]) {
					cut++
				}
				msg = "…" + msg[cut:]
			}
			q.Summary = msg
		}
	}
	return q
}

// RecordAnswer records a person's answer while no process holds the
// change lock of st: it takes the lock and, for a blocked run, appends a
// to answers in run.json and writes an answered event (see AnswerBlocked).
// For a run that isn't blocked but whose process died while its agent
// works on in a terminal, it calls terminal, which types the answer into
// that terminal when its agent waits and returns the terminal's name ("":
// it doesn't wait); the answered event then names the terminal. Anything
// else is a *NotBlockedError, and nothing is recorded. typed is the
// terminal's name when the answer went there.
func RecordAnswer(st statedir.Dir, a Answer, terminal func(rs *Run) (string, error)) (rs *Run, typed string, err error) {
	lock, err := TryChangeLock(st.RunLock())
	if err != nil {
		return nil, "", err
	}
	defer lock.Release()
	if err := WriteEditMarker(st, "tuckerbox answer"); err == nil {
		defer RemoveEditMarker(st)
	}
	rs, err = ReadRun(st.RunFile())
	if err != nil {
		return nil, "", err
	}
	// We hold the lock: anything in the inbox goes first, in order.
	DrainAnswerInbox(st)
	if rs.State == Blocked {
		a.Step, a.Round = rs.Step, rs.Round
		rs.Answers = append(rs.Answers, a)
		if err := WriteRun(st.RunFile(), rs); err != nil {
			return rs, "", err
		}
		if err := history.Append(history.File(st), AnsweredEvent(rs.Change, a, "")); err != nil {
			return rs, "", fmt.Errorf("recorded the answer in run state, but not in history: %w", err)
		}
		return rs, "", nil
	}
	if rs.Orphaned(false) && terminal != nil {
		name, err := terminal(rs)
		if err != nil {
			return rs, "", err
		}
		if name != "" {
			a.Step, a.Round = rs.Step, rs.Round
			if err := history.Append(history.File(st), AnsweredEvent(rs.Change, a, name)); err != nil {
				return rs, name, fmt.Errorf("typed the answer into terminal %s, but couldn't record it in history: %w", name, err)
			}
			return rs, name, nil
		}
	}
	return rs, "", &NotBlockedError{Change: rs.Change, State: rs.Effective(false).State}
}

// DrainAnswerInboxUnlocked moves st's inbox into history when the change
// lock is free: it takes the lock for that, and leaves the inbox to the
// lock's holder otherwise.
func DrainAnswerInboxUnlocked(st statedir.Dir) {
	lock, err := TryChangeLock(st.RunLock())
	if err != nil {
		return
	}
	defer lock.Release()
	DrainAnswerInbox(st)
}
