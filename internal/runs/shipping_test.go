package runs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

type shippingForge struct {
	forge.Client
	mu             sync.Mutex
	worktree       string
	mr             *forge.MR
	ci             string
	opened, merges int
	pins           []string
	onCheck        func()
}

func (f *shippingForge) Access(context.Context) (forge.Access, error) {
	return forge.Access{Read: true, Push: true, DefaultBranch: "main"}, nil
}
func (f *shippingForge) FindMR(context.Context, string) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mr == nil || f.mr.State != "open" {
		return nil, nil
	}
	m := *f.mr
	return &m, nil
}
func (f *shippingForge) OpenMR(_ context.Context, r forge.OpenRequest) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened++
	if f.mr != nil {
		return nil, &forge.APIError{Status: 422}
	}
	f.mr = &forge.MR{Number: 1, State: "open", URL: "https://forge.example/mr/1", Title: r.Title, Body: r.Body}
	m := *f.mr
	return &m, nil
}
func (f *shippingForge) GetMR(ctx context.Context, _ int) (*forge.MR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := *f.mr
	out, err := exec.CommandContext(ctx, "git", "-C", f.worktree, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, err
	}
	m.Head = strings.TrimSpace(string(out))
	m.Mergeable = true
	m.MergeStatus = "clean"
	return &m, nil
}
func (f *shippingForge) Checks(_ context.Context, sha, _ string) (forge.Checks, error) {
	f.mu.Lock()
	state := f.ci
	callback := f.onCheck
	f.pins = append(f.pins, sha)
	f.mu.Unlock()
	if callback != nil {
		callback()
	}
	return forge.Checks{Jobs: []forge.Job{{ID: "job1", Name: "test", State: state}}}, nil
}
func (f *shippingForge) Threads(context.Context, int) ([]forge.Thread, error) { return nil, nil }
func (f *shippingForge) Merge(_ context.Context, _ int, sha, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.merges++
	if len(f.pins) == 0 || f.pins[len(f.pins)-1] != sha {
		return fmt.Errorf("merge is not pinned to checked SHA")
	}
	f.mr.State = "merged"
	return nil
}
func shippingSetup(t *testing.T, mode config.ShipMode, archive bool, changes ...string) (*fixture, *Engine, *shippingForge) {
	t.Helper()
	f := setup(t, testenv.RepoOptions{Changes: changes, MaxParallel: 1})
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, f.dir, "clone", "--bare", f.dir, bare)
	testenv.Git(t, f.dir, "remote", "add", "origin", bare)
	f.cfg.Ship = mode
	f.cfg.Machine = "studio"
	f.cfg.Shipping.Archive = archive
	f.cfg.ForgeTarget = forge.Target{Kind: forge.GitHub, Host: "forge.example", Project: "sam/app", APIURL: "https://forge.example/api/v3"}
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go", Tick: []string{"1.1"}}})
	api := &shippingForge{worktree: f.worktree(changes[0]), ci: "green"}
	e := f.engine()
	e.Forge = func(context.Context) (forge.Client, func(string) string, error) {
		return api, func(s string) string { return s }, nil
	}
	return f, e, api
}
func TestShippingRunPersistsLeaseAndMR(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMR, false, "add-login")
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Ready || r.MR == nil || r.CI.State != "green" || r.LastPush == "" || api.opened != 1 || api.merges != 0 {
		t.Fatal(r, err, api)
	}
	saved, err := ReadRun(f.state("add-login").RunFile())
	if err != nil || saved.LastPush != r.LastPush || saved.MR.Link != r.MR.Link {
		t.Fatal(saved, err)
	}
	if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); !strings.HasPrefix(tip, r.LastPush) {
		t.Fatal(tip)
	}
	events, err := history.Read(history.File(f.state("add-login")))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, event := range events {
		kinds = append(kinds, string(event.Kind))
	}
	if !strings.Contains(strings.Join(kinds, " "), "pushed mr_opened ci_finished ready") {
		t.Fatal(kinds)
	}
}
func TestArchiveCommitPrecedesFirstPush(t *testing.T) {
	f, e, _ := shippingSetup(t, config.ShipMR, true, "add-login")
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Ready {
		t.Fatal(r, err)
	}
	files := testenv.Git(t, f.dir, "show", "--name-only", "--format=", r.LastPush)
	if !strings.Contains(files, "openspec/changes/archive/") || !strings.Contains(files, "openspec/specs/fake/spec.md") {
		t.Fatal(files)
	}
	if _, err := os.Stat(filepath.Join(r.Worktree, "openspec", "changes", "add-login")); !os.IsNotExist(err) {
		t.Fatal("active change still present", err)
	}
	if message := testenv.Git(t, f.dir, "log", "-1", "--format=%s", r.LastPush); message != "Archive add-login" {
		t.Fatal(message)
	}
}
func TestFailedArchiveBlocksWithoutPush(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMR, true, "add-login")
	testenv.WriteFile(t, filepath.Join(f.dir, "openspec", "changes", "add-login", "INVALID"), "delta does not apply")
	testenv.Git(t, f.dir, "add", ".")
	testenv.Git(t, f.dir, "commit", "-qm", "invalid archive fixture")
	testenv.Git(t, f.dir, "push", "origin", "main")
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Blocked || !strings.Contains(r.Reason, "delta does not apply") || api.opened != 0 {
		t.Fatal(r, err, api)
	}
	if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); tip != "" {
		t.Fatal(tip)
	}
}
func TestMergeRunRecordsMergedAndKeepsWorktree(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMerge, false, "add-login")
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Merged || api.merges != 1 || r.MR.State != "merged" {
		t.Fatal(r, err, api)
	}
	if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); tip != "" {
		t.Fatal(tip)
	}
	if _, err := os.Stat(r.Worktree); err != nil {
		t.Fatal(err)
	}
	events, err := history.Read(history.File(f.state("add-login")))
	if err != nil {
		t.Fatal(err)
	}
	merged := 0
	for _, event := range events {
		if event.Kind == "merged" {
			merged++
		}
	}
	if merged != 1 {
		t.Fatal("duplicate merge history", merged)
	}
}
func TestReadyMRResumeWatchesWithoutWorkflow(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMR, true, "add-login")
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Ready {
		t.Fatal(r, err)
	}
	before := len(f.env.Starts(t))
	r, err = e.Run(context.Background(), Request{Change: "add-login", Mode: Resume, Worktree: r.Worktree})
	if err != nil || r.State != Ready || len(f.env.Starts(t)) != before || api.opened != 1 {
		t.Fatal(r, err, api)
	}
}
func TestShippingWaitFreesSlotAndKeepsChangeLock(t *testing.T) {
	f, e, api := shippingSetup(t, config.ShipMR, false, "add-login", "other")
	api.ci = "pending"
	waiting := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	e.ShipSleep = func(ctx context.Context, _ time.Duration) error {
		once.Do(func() { close(waiting) })
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-proceed:
			return nil
		}
	}
	done := make(chan *Run, 1)
	errs := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	go func() { r, err := e.Run(ctx, Request{Change: "add-login"}); done <- r; errs <- err }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal("shipping never waited")
	}
	if !Active(f.state("add-login").RunLock()) {
		t.Fatal("shipping lost change lock")
	}
	cfg := *f.cfg
	cfg.Ship = config.ShipLocal
	other := f.engine()
	other.Config = &cfg
	r, err := other.Run(ctx, Request{Change: "other"})
	if err != nil || r.State != Ready {
		t.Fatal("waiting ship held slot", r, err)
	}
	api.mu.Lock()
	api.ci = "green"
	api.mu.Unlock()
	close(proceed)
	r = <-done
	if err := <-errs; err != nil || r.State != Ready {
		t.Fatal(r, err)
	}
}
