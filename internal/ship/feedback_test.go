package ship

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type feedbackForge struct {
	*fullForge
	log     string
	replies []string
	onReply func(string, string)
}

func (f *feedbackForge) Log(context.Context, forge.Job) (forge.Log, error) {
	return forge.Log{Text: f.log}, nil
}
func (f *feedbackForge) Reply(_ context.Context, _ int, thread, text string) error {
	f.replies = append(f.replies, thread+":"+text)
	if f.onReply != nil {
		f.onReply(thread, text)
	}
	return nil
}
func TestCIBecomesFindingsAndPushesFix(t *testing.T) {
	e, api, f := stage(t, config.ShipMR)
	api.ci = "red"
	e.Config.Shipping.FixCI = "fix_ci"
	e.LogDir = t.TempDir()
	forged := &feedbackForge{fullForge: api, log: "\x1b[31mprivate-token\x1b[0m\nfailed\n"}
	e.Forge = forged
	e.Mask = func(s string) string { return strings.ReplaceAll(s, "private-token", "***") }
	calls := 0
	e.Fix = func(_ context.Context, step string, findings []agent.Finding) (*agent.Result, error) {
		calls++
		if step != "fix_ci" || len(findings) != 1 || findings[0].File != "test" {
			t.Fatal(step, findings)
		}
		parts := strings.Split(findings[0].Detail, "Saved log: ")
		if len(parts) != 2 {
			t.Fatal(findings)
		}
		data, err := os.ReadFile(parts[1])
		if err != nil || strings.Contains(string(data), "private-token") || strings.Contains(string(data), "\x1b") {
			t.Fatal(string(data), err)
		}
		writeTest(t, f.one, "fixed.txt", "fixed\n")
		gitTest(t, f.one, "add", ".")
		gitTest(t, f.one, "commit", "-m", "CI fix")
		api.ci = "green"
		return &agent.Result{Status: agent.Done, Summary: "fixed CI"}, nil
	}
	out := e.Run(context.Background(), false)
	if out.State != "ready" || calls != 1 || e.Record.ShipRound != 2 || e.Record.Pending != nil {
		t.Fatal(out, calls, e.Record)
	}
	if tip, err := e.Git.Tip(context.Background()); err != nil || tip != e.Record.LastPush {
		t.Fatal(tip, err)
	}
}
func TestFixNoChangeAndRoundLimit(t *testing.T) {
	for _, scenario := range []string{"nothing", "limit", "no step"} {
		t.Run(scenario, func(t *testing.T) {
			e, api, f := stage(t, config.ShipMR)
			api.ci = "red"
			e.LogDir = t.TempDir()
			e.Config.Shipping.FixCI = "fix_ci"
			e.Config.Shipping.MaxRounds = 3
			e.Forge = &feedbackForge{fullForge: api}
			calls := 0
			if scenario == "no step" {
				e.Config.Shipping.FixCI = ""
			}
			e.Fix = func(context.Context, string, []agent.Finding) (*agent.Result, error) {
				calls++
				if scenario == "limit" {
					writeTest(t, f.one, "attempt.txt", fmt.Sprint(calls))
					gitTest(t, f.one, "add", ".")
					gitTest(t, f.one, "commit", "-m", "attempt")
				}
				return &agent.Result{Status: agent.Done}, nil
			}
			out := e.Run(context.Background(), false)
			want := "fix changed nothing"
			if scenario == "limit" {
				want = "ship limit reached"
				if calls != 3 {
					t.Fatal(calls)
				}
			}
			if scenario == "no step" {
				want = "CI failed"
				if calls != 0 {
					t.Fatal(calls)
				}
			}
			if out.State != "blocked" || out.Reason != want {
				t.Fatal(out)
			}
		})
	}
}
func TestReviewThreadsFixedRepliedAndOnlyNewCommentsRepeat(t *testing.T) {
	e, api, f := stage(t, config.ShipMR)
	e.Config.Shipping.FixReview = "review_fix"
	api.threads = []forge.Thread{{ID: "thread1", File: "internal/app/login.go", Line: 12, Comments: []forge.Comment{{ID: "c1", Author: "maintainer", Writer: true, Text: "please fix", URL: "https://forge.example/thread1"}}}, {ID: "outside", Comments: []forge.Comment{{ID: "x1", Author: "stranger", Text: "untrusted"}}}}
	forged := &feedbackForge{fullForge: api}
	e.Forge = forged
	calls := 0
	forged.onReply = func(thread, text string) {
		api.threads[0].Comments = append(api.threads[0].Comments, forge.Comment{ID: fmt.Sprintf("reply%d", len(forged.replies)), Text: text, Author: "tuckerbox"})
	}
	e.Fix = func(_ context.Context, step string, findings []agent.Finding) (*agent.Result, error) {
		calls++
		if step != "review_fix" || len(findings) != 1 || findings[0].File != "internal/app/login.go" || !strings.Contains(findings[0].Detail, "Line 12") {
			t.Fatal(step, findings)
		}
		writeTest(t, f.one, "fixed.txt", fmt.Sprint(calls))
		gitTest(t, f.one, "add", ".")
		gitTest(t, f.one, "commit", "-m", "review fix")
		return &agent.Result{Status: agent.Done, Summary: "validation added"}, nil
	}
	if out := e.Run(context.Background(), false); out.State != "ready" || calls != 1 || len(forged.replies) != 1 || api.threads[0].Resolved {
		t.Fatal(out, calls, forged.replies)
	}
	if !strings.Contains(forged.replies[0], e.Record.LastPush) {
		t.Fatal(forged.replies)
	}
	if out := e.Run(context.Background(), true); out.State != "ready" || calls != 1 {
		t.Fatal(out, calls)
	}
	api.threads[0].Comments = append(api.threads[0].Comments, forge.Comment{ID: "c2", Author: "maintainer", Writer: true, Text: "one more fix"})
	if out := e.Run(context.Background(), true); out.State != "ready" || calls != 2 || e.Record.ShipFixes != 1 {
		t.Fatal(out, calls, e.Record)
	}
}
func TestTrimLongLogAndRoundBudget(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&source, "line %d secret\n", i)
	}
	got := TrimLog(source.String(), func(s string) string { return strings.ReplaceAll(s, "secret", "***") })
	if !strings.Contains(got, "omitted 19800 lines") || !strings.Contains(got, "line 19800 ***") || !strings.HasSuffix(got, "line 19999 ***\n") || strings.Contains(got, "secret") {
		t.Fatal(got)
	}
	e, api, _ := stage(t, config.ShipMR)
	e.LogDir = t.TempDir()
	e.Forge = &feedbackForge{fullForge: api, log: strings.Repeat(strings.Repeat("x", 1024)+"\n", 1000)}
	checks := forge.Checks{}
	for i := 0; i < 8; i++ {
		checks.Jobs = append(checks.Jobs, forge.Job{ID: fmt.Sprint(i), Name: fmt.Sprint(i), State: "red"})
	}
	if _, err := e.ciFeedback(context.Background(), checks); err != nil {
		t.Fatal(err)
	}
	total := 0
	err := filepath.WalkDir(e.LogDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > jobLogLimit {
				t.Fatal(info.Size())
			}
			total += int(info.Size())
		}
		return nil
	})
	if err != nil || total > roundLogLimit {
		t.Fatal(total, err)
	}
}

type outageForge struct {
	*fullForge
	remaining int
	failure   error
	times     []time.Time
	now       func() time.Time
}

func (f *outageForge) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	f.times = append(f.times, f.now())
	if f.remaining > 0 {
		f.remaining--
		return nil, f.failure
	}
	return f.fullForge.GetMR(ctx, n)
}
func TestForgeOutageBackoffAndRateReset(t *testing.T) {
	for _, rate := range []bool{false, true} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			e, api, _ := stage(t, config.ShipMR)
			now := e.now()
			e.Now = func() time.Time { return now }
			var delays []time.Duration
			e.Sleep = func(_ context.Context, d time.Duration) error {
				delays = append(delays, d)
				now = now.Add(d)
				return nil
			}
			e.Jitter = func(d time.Duration) time.Duration { return d }
			failure := &forge.APIError{Status: 503}
			count := 3
			e.Config.Shipping.WaitTimeout = time.Hour
			if rate {
				failure = &forge.APIError{Status: 429, RetryAt: now.Add(12 * time.Minute)}
				count = 1
			}
			outage := &outageForge{fullForge: api, remaining: count, failure: failure, now: e.Now}
			e.Forge = outage
			out := e.Run(context.Background(), false)
			if out.State != "ready" {
				t.Fatal(out)
			}
			if rate {
				if len(delays) != 1 || delays[0] != 12*time.Minute {
					t.Fatal(delays)
				}
			} else {
				if len(delays) != 3 || delays[0] != 30*time.Second || delays[1] != time.Minute || delays[2] != 2*time.Minute {
					t.Fatal(delays)
				}
			}
		})
	}
}
func TestPendingCITimesOutWithFakeClock(t *testing.T) {
	e, api, _ := stage(t, config.ShipMR)
	api.ci = "pending"
	out := e.Run(context.Background(), false)
	if out.State != "blocked" || out.Reason != "waiting for CI timed out" {
		t.Fatal(out)
	}
}

func TestNetworkErrorBackoffAndPermanentRejection(t *testing.T) {
	e, _, _ := stage(t, config.ShipMR)
	e.Jitter = func(d time.Duration) time.Duration { return d / 2 }
	if delay, retry := e.retryDelay(&forge.RequestError{Message: "network unavailable"}, time.Minute); !retry || delay != 30*time.Second {
		t.Fatal(delay, retry)
	}
	if _, retry := e.retryDelay(&forge.APIError{Status: 401}, time.Minute); retry {
		t.Fatal("authentication rejection retried")
	}
}

func TestTransportOmissionCountSurvivesLogTrimming(t *testing.T) {
	raw := forge.Log{Text: strings.Repeat("retained line\n", 300), OmittedLines: 19700, Truncated: true}
	text := trimLog(raw, nil)
	if !strings.HasPrefix(text, "[omitted 19800 lines]") {
		t.Fatal(text)
	}
}
