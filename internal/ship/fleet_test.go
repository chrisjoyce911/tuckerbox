package ship

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type fleetForge struct {
	*fullForge
	beforeMerge func()
	edits       int
}

func (f *fleetForge) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	m, err := f.fullForge.GetMR(ctx, n)
	if err != nil || m == nil {
		return m, err
	}
	m.Head, err = f.git.Tip(ctx)
	return m, err
}
func (f *fleetForge) UpdateMR(_ context.Context, _ int, r forge.OpenRequest) (*forge.MR, error) {
	f.edits++
	f.mr.Body = r.Body
	f.mr.Title = r.Title
	m := *f.mr
	return &m, nil
}
func (f *fleetForge) Merge(ctx context.Context, n int, sha, method string) error {
	if f.beforeMerge != nil {
		f.beforeMerge()
	}
	tip, err := f.git.Tip(ctx)
	if err != nil {
		return err
	}
	if tip != sha {
		return &forge.APIError{Status: 409, Message: "pinned head moved"}
	}
	return f.fullForge.Merge(ctx, n, sha, method)
}
func fleetStage(t *testing.T, mode config.ShipMode) (*Engine, *fleetForge, repoFixture, fleet.Store) {
	t.Helper()
	e, api, f := stage(t, mode)
	s := fleet.Store{Dir: f.one, Now: e.Now}
	got, err := s.Acquire(context.Background(), "add-login", "studio", "run1", "add-login", false, false)
	if err != nil {
		t.Fatal(err)
	}
	e.Identity.Epoch = got.Claim.Epoch
	e.Git.Fleet = &fleet.Fence{Store: s, Claim: got.Claim}
	fake := &fleetForge{fullForge: api}
	e.Forge = fake
	return e, fake, f, s
}
func TestFleetTakeoverWaitingForCIAndMarkerRecovery(t *testing.T) {
	for _, noMR := range []bool{false, true} {
		t.Run(fmt.Sprintf("branch_without_mr=%v", noMR), func(t *testing.T) {
			e, api, f, s := fleetStage(t, config.ShipMR)
			ctx := context.Background()
			if noMR {
				head, err := e.Git.Push(ctx, "")
				if err != nil {
					t.Fatal(err)
				}
				e.Record.LastPush = head
			} else {
				out := e.Run(ctx, false)
				if out.State != "ready" {
					t.Fatal(out)
				}
			}
			old := e.Record.LastPush
			other := fleet.Store{Dir: f.two, Now: func() time.Time { return s.Now().Add(time.Hour) }}
			got, err := other.Acquire(ctx, "add-login", "mini", "run2", "add-login", true, false)
			if err != nil {
				t.Fatal(err)
			}
			// The old holder's first or fix push and its pinned merge both fail.
			gitTest(t, f.one, "commit", "--allow-empty", "-m", "stale fix")
			if _, err := e.Git.Push(ctx, old); err == nil || !strings.Contains(err.Error(), "claim taken by mini") {
				t.Fatalf("stale push: %v", err)
			}
			if err := api.Merge(ctx, 1, old, "merge"); err == nil {
				t.Fatal("stale pinned merge accepted")
			}
			// Simulate a crash just after the successful takeover, before MR editing.
			// Recovery relies only on the persisted epoch and branch push, not on the marker.
			gitTest(t, f.two, "checkout", "--detach", got.BranchTip)
			gitTest(t, f.two, "branch", "-f", "add-login", got.BranchTip)
			gitTest(t, f.two, "checkout", "add-login")
			next := *e
			next.Record = &Record{ShippingStarted: true, ArchiveDone: true, LastPush: got.BranchTip}
			next.Identity = Identity{Change: "add-login", Machine: "mini", Run: "run2", Epoch: got.Claim.Epoch}
			next.Git.Worktree = f.two
			next.Git.Fleet = &fleet.Fence{Store: other, Claim: got.Claim}
			api.git = next.Git
			out := next.Run(ctx, true)
			if out.State != "ready" {
				t.Fatal(out)
			}
			id, ok := ParseMarker(api.mr.Body)
			if !ok || id != next.Identity {
				t.Fatalf("marker not recovered: %+v %s", id, api.mr.Body)
			}
			if noMR && api.opens != 1 || !noMR && api.edits != 1 {
				t.Fatalf("opens %d edits %d", api.opens, api.edits)
			}
			if got.Claim.Epoch != 2 || next.Record.LastPush != got.BranchTip {
				t.Fatal("takeover was not resumed from fenced commit")
			}
		})
	}
}
func TestFleetReleaseAfterPremergeRenewalRefusesPinnedMerge(t *testing.T) {
	e, api, f, _ := fleetStage(t, config.ShipMerge)
	other := fleet.Store{Dir: f.two}
	api.beforeMerge = func() {
		if err := other.Release(context.Background(), "add-login", "mini", "add-login", true); err != nil {
			t.Fatal(err)
		}
	}
	out := e.Run(context.Background(), false)
	if out.State != "blocked" || !strings.Contains(out.Reason, "branch changed") {
		t.Fatal(out)
	}
	if api.mergeCalls != 0 {
		t.Fatal("forge accepted stale pin")
	}
}
func TestFleetRenewBeforeMergeRefusesLostClaim(t *testing.T) {
	e, api, f, _ := fleetStage(t, config.ShipMerge)
	other := fleet.Store{Dir: f.two}
	e.Sleep = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	api.ci = "running"
	var once bool
	// A saved state callback models takeover while waiting for CI, after the push.
	e.Save = func() error {
		if e.Record.CI != nil && e.Record.CI.State == "pending" && e.Record.LastPush != "" && !once {
			once = true
			if err := other.Release(context.Background(), "add-login", "mini", "add-login", true); err != nil {
				return err
			}
			api.ci = "green"
		}
		return nil
	}
	out := e.Run(context.Background(), false)
	if out.State != "blocked" || !(strings.Contains(out.Reason, "claim released") || strings.Contains(out.Reason, "branch changed")) || api.mergeCalls != 0 {
		t.Fatal(out, api.mergeCalls)
	}
}
