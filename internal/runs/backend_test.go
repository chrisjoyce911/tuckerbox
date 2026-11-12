package runs

import (
	"context"
	"sync"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// countingBackend runs steps headless and records each one.
type countingBackend struct {
	mu    sync.Mutex
	steps []string
}

func (b *countingBackend) Name() string { return "counting" }

func (b *countingBackend) Run(ctx context.Context, s agent.Step, opts agent.Options) (agent.Outcome, error) {
	b.mu.Lock()
	b.steps = append(b.steps, s.Step)
	b.mu.Unlock()
	return agent.Headless{}.Run(ctx, s, opts)
}

// Scenario: Headless fallback. With no backend set, each step's agent runs
// headless as a child process, as before; a backend that is set runs every
// step.
func TestEngineBackend(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"fix-login", "add-search"}})
	if b := f.engine().backend(); b.Name() != "headless" {
		t.Errorf("default backend %q, want headless", b.Name())
	}
	if r := f.run(Request{Change: "fix-login"}); r.State != Ready {
		t.Fatalf("headless run: %s %s", r.State, r.Reason)
	}
	wantStarts(t, f.env, "fix-login build 1", "fix-login review 1")

	b := &countingBackend{}
	e := f.engine()
	e.Backend = b
	r, err := e.Run(context.Background(), Request{Change: "add-search"})
	if err != nil || r.State != Ready {
		t.Fatalf("run through a backend: %v %+v", err, r)
	}
	if len(b.steps) != 2 || b.steps[0] != "build" || b.steps[1] != "review" {
		t.Errorf("backend ran %v, want build and review", b.steps)
	}
}
