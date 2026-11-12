package ship

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type fullForge struct {
	*fakeForge
	git                    Git
	state, ci, mergeStatus string
	mergeCalls             int
	mergedSHA, method      string
	mergeError             error
	threads                []forge.Thread
}

func (f *fullForge) Access(context.Context) (forge.Access, error) {
	return forge.Access{Read: true, Push: true, DefaultBranch: "main"}, nil
}
func (f *fullForge) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	m, err := f.FindMR(ctx, "")
	if err != nil || m == nil {
		return m, err
	}
	m.Head, _ = f.git.Head(ctx)
	m.State = f.state
	if m.State == "" {
		m.State = "open"
	}
	m.Mergeable = f.mergeStatus == "clean"
	m.MergeStatus = f.mergeStatus
	return m, nil
}
func (f *fullForge) Checks(context.Context, string, string) (forge.Checks, error) {
	if f.ci == "none" {
		return forge.Checks{}, nil
	}
	return forge.Checks{Jobs: []forge.Job{{ID: "job1", Name: "test", State: f.ci}}}, nil
}
func (f *fullForge) Log(context.Context, forge.Job) (string, error)       { return "test output", nil }
func (f *fullForge) Threads(context.Context, int) ([]forge.Thread, error) { return f.threads, nil }
func (f *fullForge) Reply(context.Context, int, string, string) error     { return nil }
func (f *fullForge) Merge(_ context.Context, _ int, sha, method string) error {
	f.mergeCalls++
	f.mergedSHA = sha
	f.method = method
	return f.mergeError
}
func stage(t *testing.T, mode config.ShipMode) (*Engine, *fullForge, repoFixture) {
	t.Helper()
	f := repositories(t)
	c := f.claim(f.one, "studio")
	api := &fullForge{fakeForge: &fakeForge{}, git: c.Git, ci: "green", mergeStatus: "clean"}
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.FixedZone("AEST", 10*3600))
	e := &Engine{Config: &config.Config{Ship: mode, Shipping: config.Shipping{Archive: false, MergeMethod: "merge", WaitTimeout: 10 * time.Minute}}, Git: c.Git, Identity: c.Identity, Forge: api, Record: &Record{}, Save: func() error { return nil }, Commit: func(context.Context, string) (bool, error) { return false, nil }, Request: forge.OpenRequest{Title: "add login", Body: "proposal", Base: "main"}, Now: func() time.Time { return now }}
	e.Sleep = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	return e, api, f
}
func TestMRModeStopsGreenWithoutMerging(t *testing.T) {
	e, api, _ := stage(t, config.ShipMR)
	released := false
	e.ReleaseSlot = func() error { released = true; return nil }
	out := e.Run(context.Background(), false)
	if out.State != "ready" || api.mergeCalls != 0 || e.Record.MR == nil || e.Record.CI.State != "green" || !released {
		t.Fatal(out, e.Record, api.mergeCalls, released)
	}
}
func TestMergePinnedToGreenHead(t *testing.T) {
	for _, method := range []string{"merge", "squash", "rebase"} {
		t.Run(method, func(t *testing.T) {
			e, api, _ := stage(t, config.ShipMerge)
			e.Config.Shipping.MergeMethod = method
			out := e.Run(context.Background(), false)
			if out.State != "merged" || api.mergeCalls != 1 || api.mergedSHA != e.Record.LastPush || api.method != method {
				t.Fatal(out, api, e.Record)
			}
			tip, err := e.Git.Tip(context.Background())
			if err != nil || tip != "" {
				t.Fatal("merged branch was not deleted", tip, err)
			}
		})
	}
}
func TestHeadMovedBeforeMerge(t *testing.T) {
	e, api, _ := stage(t, config.ShipMerge)
	api.mergeError = &forge.APIError{Status: 409, Message: "head moved"}
	out := e.Run(context.Background(), false)
	if out.State != "blocked" || out.Reason != "branch changed on the forge" || api.mergedSHA != e.Record.LastPush {
		t.Fatal(out, api)
	}
}
func TestArchiveBeforePushAndFailurePushesNothing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			e, api, f := stage(t, config.ShipMR)
			e.Config.Shipping.Archive = true
			archived := false
			committed := false
			e.Archive = func(ctx context.Context, change string) (string, error) {
				tip, err := e.Git.Tip(ctx)
				if err != nil || tip != "" {
					t.Fatal("push before archive", tip, err)
				}
				if fail {
					return "", fmt.Errorf("delta does not apply")
				}
				archived = true
				return "openspec/changes/archive/2026-11-12-add-login", nil
			}
			e.Commit = func(ctx context.Context, msg string) (bool, error) {
				if strings.HasPrefix(msg, "Archive ") {
					if !archived {
						t.Fatal("archive commit before archive")
					}
					writeTest(t, f.one, "archive.txt", "archive\n")
					gitTest(t, f.one, "add", ".")
					gitTest(t, f.one, "commit", "-m", msg)
					committed = true
				}
				return committed, nil
			}
			out := e.Run(context.Background(), false)
			if fail {
				tip, _ := e.Git.Tip(context.Background())
				if out.State != "blocked" || !strings.Contains(out.Reason, "delta does not apply") || tip != "" || api.opens != 0 {
					t.Fatal(out, tip, api)
				}
			} else {
				if out.State != "ready" || !committed || e.Record.ArchivePath == "" {
					t.Fatal(out, e.Record)
				}
			}
		})
	}
}
func TestProtectionPendingMissingCIAndClosedMR(t *testing.T) {
	for _, scenario := range []string{"approval", "no CI", "closed", "merged elsewhere", "review waiting"} {
		t.Run(scenario, func(t *testing.T) {
			e, api, _ := stage(t, config.ShipMerge)
			wantState := "blocked"
			wantReason := ""
			switch scenario {
			case "approval":
				api.mergeStatus = "blocked"
				wantReason = "waiting for approval timed out"
			case "no CI":
				api.ci = "none"
				wantReason = "no checks reported"
			case "closed":
				api.state = "closed"
				wantReason = "merge request closed"
			case "merged elsewhere":
				api.state = "merged"
				wantState = "merged"
			case "review waiting":
				api.threads = []forge.Thread{{ID: "thread1", Comments: []forge.Comment{{ID: "comment1", Author: "writer", Writer: true}}}}
				wantReason = "review findings need a fix step"
			}
			out := e.Run(context.Background(), false)
			if out.State != wantState || out.Reason != wantReason || api.mergeCalls != 0 {
				t.Fatal(out, api.mergeCalls)
			}
		})
	}
}
func TestNoCommitsBeyondBase(t *testing.T) {
	e, api, f := stage(t, config.ShipMR)
	gitTest(t, f.one, "reset", "--hard", "main")
	out := e.Run(context.Background(), false)
	if out.State != "blocked" || out.Reason != "branch has no commits beyond the base" || api.opens != 0 {
		t.Fatal(out, api)
	}
}
func TestCIStatesAndReviewEligibility(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		checks forge.Checks
		age    time.Duration
		want   string
	}{{forge.Checks{}, 0, "pending"}, {forge.Checks{}, 5 * time.Minute, "none"}, {forge.Checks{Jobs: []forge.Job{{Name: "test", State: "green"}}}, 0, "green"}, {forge.Checks{Jobs: []forge.Job{{Name: "test", State: "red"}}}, 0, "red"}, {forge.Checks{Jobs: []forge.Job{{Name: "test", State: "green"}}, Required: []string{"missing"}}, time.Hour, "pending"}} {
		if got := CIState(tt.checks, now.Add(-tt.age), now); got != tt.want {
			t.Fatal(got, tt.want)
		}
	}
	thread := forge.Thread{ID: "thread", Comments: []forge.Comment{{Author: "stranger"}}}
	if EligibleThread(thread, nil) {
		t.Fatal("outside reviewer allowed")
	}
	if !EligibleThread(thread, []string{"stranger"}) {
		t.Fatal("named bot ignored")
	}
	thread.Resolved = true
	if EligibleThread(thread, []string{"stranger"}) {
		t.Fatal("resolved thread allowed")
	}
}
