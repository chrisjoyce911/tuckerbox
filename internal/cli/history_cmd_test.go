package cli

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

type historyDoc struct {
	Version int    `json:"version"`
	Change  string `json:"change"`
	Events  []struct {
		Time, Kind, Step, Agent, Machine, To, Status, Summary, Reason string
		Round                                                         int
		Findings                                                      []struct{ Severity, File, Detail string }
	} `json:"events"`
}

func (f *cliFixture) history(change string) historyDoc {
	f.t.Helper()
	res := f.tb("history", change, "--json")
	if res.code != 0 {
		f.t.Fatalf("history --json: exit %d: %s", res.code, res.stderr)
	}
	var doc historyDoc
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		f.t.Fatalf("history --json: %v\n%s", err, res.stdout)
	}
	return doc
}

// Scenario: No run yet. The history of a change that has never run is
// empty, and tuckerbox exits 0.
func TestHistoryNoRunYet(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	res := f.tb("history", "fix-login")
	if res.code != 0 || !strings.Contains(res.stdout, "no history for fix-login") {
		t.Errorf("text: %d %q %q", res.code, res.stdout, res.stderr)
	}
	res = f.tb("history", "fix-login", "--json")
	if res.code != 0 || !strings.Contains(res.stdout, `"events": []`) {
		t.Errorf("json: %d %q %q", res.code, res.stdout, res.stderr)
	}
	if doc := f.history("fix-login"); doc.Version != HistoryVersion || doc.Change != "fix-login" || len(doc.Events) != 0 {
		t.Errorf("doc %+v", doc)
	}
}

// The table shows events oldest first in the machine's local time zone,
// with a rejection's findings under it; --json gives the same events with
// RFC 3339 times.
func TestHistoryAfterARejection(t *testing.T) {
	zone := "Asia/Kolkata" // UTC+05:30 all year, unlike the test machine
	t.Setenv("TZ", zone)
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {
		Status: "rejected", Summary: "not yet",
		Findings: []testenv.Finding{
			{Severity: "high", File: "login.go", Detail: "password is logged"},
			{Severity: "low", Detail: "no changelog entry"},
		},
	}})
	if res := f.tb("run", "--foreground", "fix-login"); res.code != 0 {
		t.Fatalf("run: %d %s %s", res.code, res.stdout, res.stderr)
	}

	doc := f.history("fix-login")
	var kinds []string
	for _, e := range doc.Events {
		kinds = append(kinds, e.Kind)
		ts, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			t.Fatalf("time %q: %v", e.Time, err)
		}
		if runtime.GOOS != "windows" { // Go ignores TZ on Windows
			if _, off := ts.Zone(); off != 5*3600+1800 {
				t.Errorf("time %s is not in %s", e.Time, zone)
			}
		}
	}
	want := "started queued step_started step_finished handoff step_started step_finished rejected " +
		"step_started step_finished handoff step_started step_finished ready"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds:\n%s\nwant:\n%s", got, want)
	}
	rej := doc.Events[7]
	if rej.Step != "review" || rej.Round != 1 || rej.To != "build" || len(rej.Findings) != 2 || rej.Agent == "" || rej.Machine == "" {
		t.Errorf("rejection %+v", rej)
	}

	res := f.tb("history", "fix-login")
	if res.code != 0 {
		t.Fatalf("history: %d %s", res.code, res.stderr)
	}
	lines := strings.Split(strings.TrimRight(res.stdout, "\n"), "\n")
	if !strings.HasPrefix(lines[0], "TIME") || !strings.Contains(lines[0], "EVENT") {
		t.Errorf("header %q", lines[0])
	}
	first, _ := time.Parse(time.RFC3339, doc.Events[0].Time)
	if !strings.HasPrefix(lines[1], first.Format("2006-01-02 15:04:05")) || !strings.Contains(lines[1], "started") {
		t.Errorf("first row %q, want time %s", lines[1], first.Format("2006-01-02 15:04:05"))
	}
	out := res.stdout
	for _, s := range []string{"rejected: not yet", "-> build", "    - [high] login.go: password is logged", "    - [low] no changelog entry", "ready"} {
		if !strings.Contains(out, s) {
			t.Errorf("table lacks %q:\n%s", s, out)
		}
	}
	// Findings are listed once, under the rejection.
	if n := strings.Count(out, "password is logged"); n != 1 {
		t.Errorf("finding listed %d times:\n%s", n, out)
	}
}

func TestHistoryUsage(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{})
	for _, args := range [][]string{{"history"}, {"history", "a", "b"}, {"history", "a", "--bogus"}} {
		if res := f.tb(args...); res.code != 2 {
			t.Errorf("%v: exit %d", args, res.code)
		}
	}
	if res := f.tb("history", "../x"); res.code != 1 {
		t.Errorf("bad id: exit %d %s", res.code, res.stderr)
	}
}

// Review finding: a change that never ran in this repository has an empty
// history, even when another repository on the machine ran a change with
// the same id.
func TestHistoryScopedToThisRepository(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	other := tools.NewRepo(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	if res := tbIn(t, other, "run", "--foreground", "fix-login"); res.code != 0 {
		t.Fatalf("run in the other repository: %d %s", res.code, res.stderr)
	}
	if doc := f.history("fix-login"); len(doc.Events) != 0 {
		t.Errorf("history here shows the other repository's %d events", len(doc.Events))
	}
	if res := f.tb("history", "fix-login"); res.code != 0 || !strings.Contains(res.stdout, "no history for fix-login") {
		t.Errorf("text: %d %q", res.code, res.stdout)
	}
	// The other repository still sees its own.
	if res := tbIn(t, other, "history", "fix-login", "--json"); res.code != 0 || !strings.Contains(res.stdout, `"ready"`) {
		t.Errorf("other repository: %d %s", res.code, res.stdout)
	}
}

// Review findings: findings on a result that isn't a rejection (here a
// blocked build) are listed, and the last step's summary is shown.
func TestHistoryTextShowsEveryResult(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1": {Status: "blocked", Summary: "need a decision",
			Findings: []testenv.Finding{{Severity: "medium", File: "login.go", Detail: "choose a credential store"}}},
		"review": {Summary: "verified credential rotation"},
	})
	if res := f.tb("run", "--foreground", "fix-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	if res := f.tb("resume", "--foreground", "fix-login"); res.code != 0 {
		t.Fatalf("resume: %d %s", res.code, res.stderr)
	}
	res := f.tb("history", "fix-login")
	if res.code != 0 {
		t.Fatalf("history: %d %s", res.code, res.stderr)
	}
	for _, s := range []string{
		"blocked: need a decision",
		"    - [medium] login.go: choose a credential store",
		"done: verified credential rotation",
	} {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("table lacks %q:\n%s", s, res.stdout)
		}
	}
}
