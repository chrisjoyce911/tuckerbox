package autopilot

import (
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
)

func ev(kind history.Kind, by string) history.Event { return history.Event{Kind: kind, By: by} }

// The crash streak counts autopilot's resumes since the last step result
// or person action, whatever the rounds (formal/autopilot.tla, CrashBudget).
func TestCrashStreak(t *testing.T) {
	const a = By
	cases := []struct {
		name   string
		events []history.Event
		want   int
	}{
		{"none", nil, 0},
		{"started by autopilot", []history.Event{ev(history.Started, a), ev(history.StepStarted, "")}, 0},
		{"one resume", []history.Event{ev(history.Started, a), ev(history.Stopped, ""), ev(history.Resumed, a)}, 1},
		{"across rounds", []history.Event{ev(history.Started, ""), ev(history.Resumed, a), ev(history.StepStarted, ""), ev(history.Stopped, ""), ev(history.Resumed, a)}, 2},
		{"progress renews", []history.Event{ev(history.Resumed, a), ev(history.StepFinished, "")}, 0},
		{"a person renews", []history.Event{ev(history.Resumed, a), ev(history.Resumed, "")}, 0},
		{"person rerun", []history.Event{ev(history.Resumed, a), ev(history.Rerun, "")}, 0},
		{"autopilot next group keeps count", []history.Event{ev(history.Resumed, a), ev(history.ScopeStarted, a)}, 1},
	}
	for _, c := range cases {
		if got := CrashStreak(c.events); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}
