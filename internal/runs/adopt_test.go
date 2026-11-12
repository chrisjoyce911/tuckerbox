package runs

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// fakeAdopter is a terminal-like backend whose one agent outlives the run
// process: its first Run leaves the agent live and reports that its
// terminal would not end, as a run process that died would leave it.
type fakeAdopter struct {
	mu       sync.Mutex
	live     bool
	labels   map[string]string
	runs     int
	ends     int
	endFails bool
	// saved are the "step round name" of each SaveHistory, which happens
	// only after an end.
	saved []string
}

var _ agent.Adopter = (*fakeAdopter)(nil)

func (b *fakeAdopter) SaveHistory(logPath, step string, round int, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.live {
		return errors.New("saving the history of a live terminal")
	}
	b.saved = append(b.saved, step+" "+strconv.Itoa(round)+" "+name)
	return nil
}

func (b *fakeAdopter) Name() string { return "terminal" }

func (b *fakeAdopter) Run(ctx context.Context, s agent.Step, opts agent.Options) (agent.Outcome, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.runs++
	b.live = true
	b.labels = map[string]string{agent.LabelChange: s.Change, agent.LabelStep: s.Step, agent.LabelRound: strconv.Itoa(s.Round)}
	if opts.OnTerminal != nil {
		opts.OnTerminal(s.Term.Name, "t1")
	}
	return agent.Outcome{}, &agent.EndError{Name: s.Term.Name, Err: errors.New("the run process died")}
}

func (b *fakeAdopter) LiveAgent(ctx context.Context, name string) (*agent.HCTerminal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.live {
		return nil, nil
	}
	return &agent.HCTerminal{Name: name, State: "live", Labels: b.labels}, nil
}

func (b *fakeAdopter) FindAgent(ctx context.Context, name string) (*agent.HCTerminal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.labels == nil {
		return nil, nil
	}
	state := "exited"
	if b.live {
		state = "live"
	}
	return &agent.HCTerminal{Name: name, State: state, Labels: b.labels}, nil
}

// exit ends the fake agent by itself, as an agent that finishes while no
// run process watches it: its terminal stays, ended.
func (b *fakeAdopter) exit() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.live = false
}

func (b *fakeAdopter) EndAgent(ctx context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ends++
	if b.endFails {
		return errors.New("fake kill failure")
	}
	b.live = false
	return nil
}

func (b *fakeAdopter) isLive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.live
}

// A stop that lands after a resume has taken its adopted agent's slot back,
// but before the step watches the agent, ends the agent's terminal before
// the run is recorded stopped. When the terminal will not end, nothing is
// recorded stopped: the run keeps its terminal and slot for a later stop.
func TestStopAfterAdoptionTakesTheSlot(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run("endFails="+strconv.FormatBool(fail), func(t *testing.T) {
			f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}, Interactive: true})
			b := &fakeAdopter{}
			e := f.engine()
			e.Backend = b
			r, err := e.Run(context.Background(), Request{Change: "add-login"})
			var endErr *agent.EndError
			if !errors.As(err, &endErr) || r.State != Running || r.Terminal == nil || r.Slot == nil {
				t.Fatalf("first run: %v %+v", err, r)
			}
			name := r.Terminal.Name

			b.endFails = fail
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			e = f.engine()
			e.Backend = b
			e.afterSlot = cancel
			r, err = e.Run(ctx, Request{Change: "add-login", Mode: Resume})
			if b.runs != 1 {
				t.Errorf("backend ran %d steps, want none after the stop", b.runs-1)
			}
			cur := f.current("add-login")
			if fail {
				if !errors.As(err, &endErr) {
					t.Errorf("resume with a terminal that will not end: %v", err)
				}
				if !b.isLive() {
					t.Fatal("the fake agent ended")
				}
				if cur.State != Running || cur.Terminal == nil || cur.Terminal.Name != name || cur.Slot == nil {
					t.Errorf("run after the failed end: %s terminal %+v slot %v", cur.State, cur.Terminal, cur.Slot)
				}
				if len(b.saved) != 0 {
					t.Errorf("history saved while the agent lives: %v", b.saved)
				}
				return
			}
			if err != nil || r.State != Stopped {
				t.Fatalf("resume: %v %+v", err, r)
			}
			if b.isLive() || b.ends != 1 {
				t.Errorf("agent live %v after %d ends", b.isLive(), b.ends)
			}
			if cur.State != Stopped || cur.Terminal != nil || cur.Round != 1 {
				t.Errorf("run %s round %d terminal %+v", cur.State, cur.Round, cur.Terminal)
			}
			if want := "build 1 " + name; len(b.saved) != 1 || b.saved[0] != want {
				t.Errorf("histories saved %v, want [%s]", b.saved, want)
			}
		})
	}
}

// An orphaned agent that ended by itself before resume: resume does not
// adopt it, and saves its terminal's session to the step log before the
// new round's agent replaces the terminal.
func TestResumeSavesAnEndedOrphansSession(t *testing.T) {
	f := setup(t, testenv.RepoOptions{Changes: []string{"add-login"}, Interactive: true})
	b := &fakeAdopter{}
	e := f.engine()
	e.Backend = b
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	var endErr *agent.EndError
	if !errors.As(err, &endErr) || r.Terminal == nil {
		t.Fatalf("first run: %v %+v", err, r)
	}
	name := r.Terminal.Name
	b.exit()

	e = f.engine()
	e.Backend = b
	_, _ = e.Run(context.Background(), Request{Change: "add-login", Mode: Resume})
	if b.runs != 2 {
		t.Errorf("backend ran %d steps, want a new round", b.runs)
	}
	if want := "build 1 " + name; len(b.saved) != 1 || b.saved[0] != want {
		t.Errorf("histories saved %v, want [%s]", b.saved, want)
	}
	if cur := f.current("add-login"); cur.Round != 2 {
		t.Errorf("round %d, want 2", cur.Round)
	}
}
