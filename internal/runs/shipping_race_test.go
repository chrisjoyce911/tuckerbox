package runs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// Unlike shippingForge's per-worktree stub, this forge observes the shared
// remote branch, just as a forge does when independent clones race.
type racingForge struct {
	*shippingForge
	repo string
}

func (f *racingForge) GetMR(ctx context.Context, _ int) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := *f.mr
	out, err := exec.CommandContext(ctx, "git", "-C", f.repo, "ls-remote", "origin", "refs/heads/add-login").Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		m.Head = fields[0]
	}
	m.Mergeable = true
	m.MergeStatus = "clean"
	return &m, nil
}
func TestTwoEnginesRaceForOneShippingClaim(t *testing.T) {
	f, e, _ := shippingSetup(t, config.ShipMR, false, "add-login")
	clone := filepath.Join(t.TempDir(), "mini")
	remote := testenv.Git(t, f.dir, "remote", "get-url", "origin")
	testenv.Git(t, f.dir, "clone", remote, clone)
	testenv.Git(t, clone, "config", "user.name", "Test")
	testenv.Git(t, clone, "config", "user.email", "test@example.invalid")
	second := open(t, f.env, clone)
	cfg := *f.cfg
	cfg.Machine = "mini"
	second.cfg = &cfg
	e2 := second.engine()
	e2.StateHome = t.TempDir()
	api := &racingForge{shippingForge: &shippingForge{ci: "green"}, repo: f.dir}
	connect := func(context.Context) (forge.Client, func(string) string, error) {
		return api, func(s string) string { return s }, nil
	}
	e.Forge = connect
	e2.Forge = connect
	gate := f.gate("race-review")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go", Tick: []string{"1.1"}}, "review": {WaitFor: gate}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		run     *Run
		err     error
		machine string
	}
	done := make(chan result, 2)
	for i, engine := range []*Engine{e, e2} {
		go func(i int, engine *Engine) {
			r, err := engine.Run(ctx, Request{Change: "add-login"})
			done <- result{r, err, []string{"studio", "mini"}[i]}
		}(i, engine)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		reviews := 0
		for _, start := range f.env.Starts(t) {
			if strings.Contains(start, " review ") {
				reviews++
			}
		}
		if reviews == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("both engines did not reach review")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Distinct data makes the two push candidates distinct even within one second.
	for i, fixture := range []*fixture{f, second} {
		testenv.WriteFile(t, filepath.Join(fixture.worktree("add-login"), "machine.txt"), []string{"studio", "mini"}[i])
	}
	if err := os.WriteFile(gate, []byte("go"), 0644); err != nil {
		t.Fatal(err)
	}
	a, b := <-done, <-done
	if a.err != nil || b.err != nil {
		t.Fatal(a.err, b.err)
	}
	if a.run.State == Blocked {
		a, b = b, a
	}
	if a.run.State != Ready || b.run.State != Blocked {
		t.Fatal(a.run, b.run)
	}
	if !strings.Contains(b.run.Reason, "claimed by") && !strings.Contains(b.run.Reason, "branch changed") && !strings.Contains(b.run.Reason, "remote branch exists") {
		t.Fatal(b.run.Reason)
	}
	api.mu.Lock()
	opened := api.opened
	marker, ok := ship.ParseMarker(api.mr.Body)
	api.mu.Unlock()
	if opened != 1 || !ok || marker.Machine != a.machine {
		t.Fatal(opened, marker, a.machine)
	}
	if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); !strings.HasPrefix(tip, a.run.LastPush) {
		t.Fatal(tip, a.run.LastPush)
	}
	if b.run.LastPush != "" {
		t.Fatal("loser recorded a successful push", b.run.LastPush)
	}
}
