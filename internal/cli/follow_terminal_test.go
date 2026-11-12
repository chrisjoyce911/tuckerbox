package cli

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
)

// scriptedTerms is a herdr-core whose terminal called name is replaced by
// the next one in terms after each hc get, as when a step hands its
// terminal name to the next step between two looks.
type scriptedTerms struct {
	mu    sync.Mutex
	terms []*agent.HCTerminal // what each hc get sees, in turn; the last stays
	outs  map[string]string   // each terminal's output, by address
	gets  int
}

func (s *scriptedTerms) cur() *agent.HCTerminal {
	i := s.gets
	if i >= len(s.terms) {
		i = len(s.terms) - 1
	}
	return s.terms[i]
}

func (s *scriptedTerms) Get(_ context.Context, _ string) (*agent.HCTerminal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.cur()
	s.gets++
	c := *t
	return &c, nil
}

func (s *scriptedTerms) Read(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []byte(s.outs[s.cur().Address]), nil
}

func stepTerm(addr, step, state string) *agent.HCTerminal {
	return &agent.HCTerminal{Address: addr, Name: "tb-x", State: state, Labels: map[string]string{
		agent.LabelChange: "add-login", agent.LabelStep: step, agent.LabelRound: "1"}}
}

// Following the build's terminal while the review's replaces it under the
// same name, between two polls or between a poll's look and its read:
// logs ends with the build and never shows the review's terminal.
func TestFollowTerminalStepHandoff(t *testing.T) {
	build := stepTerm("t1", "build", "live")
	review := stepTerm("t2", "review", "live")
	outs := map[string]string{"t1": "build says hello\n", "t2": "REVIEW OUTPUT\n"}
	for _, tc := range []struct {
		name  string
		terms []*agent.HCTerminal
	}{
		// read+get, then the poll's get sees the review's terminal.
		{"between polls", []*agent.HCTerminal{build, review}},
		// read+get, poll get still the build, then the read races the
		// handoff and the get after it sees the review's.
		{"during a read", []*agent.HCTerminal{build, build, review}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &scriptedTerms{terms: tc.terms, outs: outs}
			var w bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := followTerminal(ctx, src, "tb-x", build, "add-login", "build", 1, &w, true, time.Millisecond, func() bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			if ctx.Err() != nil {
				t.Fatal("followTerminal did not end with the build's terminal")
			}
			if got := w.String(); !strings.Contains(got, "build says hello") || strings.Contains(got, "REVIEW") {
				t.Errorf("followed output %q", got)
			}
		})
	}
}

// The pinned terminal's last output is shown when it ends.
func TestFollowTerminalEnds(t *testing.T) {
	build := stepTerm("t1", "build", "live")
	src := &scriptedTerms{terms: []*agent.HCTerminal{build, build, build, stepTerm("t1", "build", "exited")},
		outs: map[string]string{"t1": "build says hello\n"}}
	var w bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := followTerminal(ctx, src, "tb-x", build, "add-login", "build", 1, &w, true, time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || w.String() != "build says hello\n" {
		t.Errorf("followed output %q (ctx %v)", w.String(), ctx.Err())
	}
}
