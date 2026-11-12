package runs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

func blockedState(t *testing.T) statedir.Dir {
	t.Helper()
	st := statedir.Dir(t.TempDir())
	rs := &Run{Change: "c", State: Blocked, Step: "build", Round: 2, History: []Entry{
		{Step: "build", Round: 1, Status: agent.Blocked, Summary: "older question"},
		{Step: "build", Round: 2, Status: agent.Blocked, Summary: "keep rollback?"},
	}}
	if err := WriteRun(st.RunFile(), rs); err != nil {
		t.Fatal(err)
	}
	return st
}

// The inbox goes into history oldest first, once, and only an answer does.
func TestAnswerInboxDrains(t *testing.T) {
	st := blockedState(t)
	base := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	for _, i := range []int{2, 0, 1} {
		a := Answer{Text: string(rune('a' + i)), By: "Sam", At: base.Add(time.Duration(i) * time.Second), Step: "build", Round: 2}
		if err := PostAnswer(st, AnsweredEvent("c", a, "tb-x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(AnswerInbox(st), "junk.json"), []byte(`{"kind":"stopped"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	DrainAnswerInboxUnlocked(st)
	DrainAnswerInbox(st)
	events, _ := history.Read(history.File(st))
	if len(events) != 3 {
		t.Fatalf("events %+v", events)
	}
	for i, e := range events {
		if e.Kind != history.Answered || e.Text != string(rune('a'+i)) || e.Terminal != "tb-x" || e.By != "Sam" {
			t.Errorf("event %d: %+v", i, e)
		}
	}
	if entries, _ := os.ReadDir(AnswerInbox(st)); len(entries) != 0 {
		t.Errorf("inbox left %d files", len(entries))
	}
}

// Answers are recorded only under the change lock, for the blocked step
// and round, and the brief groups them under the question they answer.
func TestRecordAnswerAndPending(t *testing.T) {
	st := blockedState(t)
	lock, err := TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RecordAnswer(st, Answer{Text: "x", By: "Sam", At: time.Now()}, nil); !errors.Is(err, ErrLocked) {
		t.Errorf("with the lock held: %v", err)
	}
	lock.Release()
	for _, text := range []string{"first", "second"} {
		if _, _, err := RecordAnswer(st, Answer{Text: text, By: "Sam", At: time.Now()}, nil); err != nil {
			t.Fatal(err)
		}
	}
	rs, _ := ReadRun(st.RunFile())
	if rs.State != Blocked || len(rs.Answers) != 2 || rs.Answers[0].Round != 2 || rs.Answers[1].Step != "build" {
		t.Fatalf("run %+v", rs)
	}
	p := rs.PendingAnswers("build")
	if len(p) != 1 || p[0].Question.Summary != "keep rollback?" || len(p[0].Answers) != 2 || p[0].Answers[0].Text != "first" {
		t.Errorf("pending %+v", p)
	}
	if len(rs.PendingAnswers("review")) != 0 {
		t.Error("answers for another step")
	}
	rs.useAnswers("build")
	if len(rs.PendingAnswers("build")) != 0 {
		t.Error("used answers still pending")
	}
	q := Ask(rs, st, "")
	if q == nil || q.Kind != QuestionBlocked || q.Summary != "keep rollback?" || len(q.Answers) != 2 {
		t.Errorf("ask %+v", q)
	}

	// Not blocked: refused, nothing recorded.
	rs.State = Ready
	if err := WriteRun(st.RunFile(), rs); err != nil {
		t.Fatal(err)
	}
	var nb *NotBlockedError
	if _, _, err := RecordAnswer(st, Answer{Text: "x"}, nil); !errors.As(err, &nb) || nb.State != Ready {
		t.Errorf("ready run: %v", err)
	}
	if rs2, _ := ReadRun(st.RunFile()); len(rs2.Answers) != 2 {
		t.Errorf("answers %d", len(rs2.Answers))
	}
}

func TestCheckAnswer(t *testing.T) {
	if got, err := CheckAnswer("  yes\n"); err != nil || got != "yes" {
		t.Errorf("%q %v", got, err)
	}
	if _, err := CheckAnswer(" \n "); err == nil {
		t.Error("empty accepted")
	}
	big := make([]byte, MaxAnswer+1)
	for i := range big {
		big[i] = 'x'
	}
	if _, err := CheckAnswer(string(big)); err == nil {
		t.Error("over 16 KiB accepted")
	}
	if _, err := CheckAnswer(string(big[:MaxAnswer])); err != nil {
		t.Error(err)
	}
}
