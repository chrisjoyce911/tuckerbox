package board

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestTimestampUsesLocalZone(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })
	for _, offset := range []int{-8 * 3600, 3600, 19800} {
		time.Local = time.FixedZone("test local", offset)
		instant := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
		got, err := time.Parse(time.RFC3339, timestamp(instant))
		if err != nil || !got.Equal(instant) {
			t.Fatalf("%v %v", got, err)
		}
		if _, zone := got.Zone(); zone != offset {
			t.Fatalf("offset=%d want %d", zone, offset)
		}
	}
	if timestamp(time.Time{}) != "" {
		t.Fatal("zero time should be blank")
	}
}

func TestBrokenChangeDoesNotHideOtherCards(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"good", "broken"}})
	testenv.WriteFile(t, filepath.Join(root, "openspec", "changes", "broken", "tuckerbox.toml"), "[unterminated\n")
	d := read(t, root, "broken")
	if len(d.Cards) != 2 || card(t, d, "good").Error != "" || card(t, d, "broken").Error == "" || d.Detail == nil {
		t.Fatal(d)
	}
}

func TestMissingWorktreeAndArchivedWorktreeChange(t *testing.T) {
	for _, missing := range []bool{true, false} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			testTools.Setup(t)
			root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"good", "broken"}})
			wt := filepath.Join(filepath.Dir(root), "repo-worktrees", "broken")
			testenv.Git(t, root, "worktree", "add", "-qb", "broken", wt)
			if missing {
				if err := os.RemoveAll(wt); err != nil {
					t.Fatal(err)
				}
			} else {
				st, err := statedir.Resolve(context.Background(), wt)
				if err != nil {
					t.Fatal(err)
				}
				if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: "broken", Step: "build", State: runs.Stopped, Round: 1, Worktree: wt}); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Join(wt, "openspec", "changes", "broken")); err != nil {
					t.Fatal(err)
				}
			}
			d := read(t, root, "broken")
			if len(d.Cards) != 2 || card(t, d, "good").Error != "" || card(t, d, "broken").Error == "" || card(t, d, "broken").EditFolder != wt {
				t.Fatal(d)
			}
		})
	}
}

func TestOneWorktreeScanAndNoArchiveGitChecks(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"one", "two", "three"}})
	for _, id := range []string{"one", "two", "three"} {
		testenv.Git(t, root, "worktree", "add", "-qb", id, filepath.Join(filepath.Dir(root), "repo-worktrees", id))
	}
	for i := 1; i <= 20; i++ {
		folder := filepath.Join(root, "openspec", "changes", "archive", fmt.Sprintf("2026-11-%02d-old-%02d", i, i))
		testenv.WriteFile(t, filepath.Join(folder, "tasks.md"), "- [x] 1.1 Done\n")
		testenv.WriteFile(t, filepath.Join(folder, "tuckerbox.toml"), "[approval]\nhash = 'obsolete'\nby = 'Reviewer'\n")
		// Hashing this proposal would fail: archived approvals must not be judged.
		if err := os.MkdirAll(filepath.Join(folder, "proposal.md"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	trace := filepath.Join(t.TempDir(), "git-trace.txt")
	t.Setenv("GIT_TRACE", filepath.ToSlash(trace))
	d := read(t, root, "")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if got := strings.Count(text, "built-in: git worktree list --porcelain"); got != 1 {
		t.Fatalf("worktree scans=%d\n%s", got, text)
	}
	if got := strings.Count(text, "built-in: git rev-parse --git-dir"); got != 4 {
		t.Fatalf("state resolutions=%d\n%s", got, text)
	}
	if strings.Contains(text, "archive/") {
		t.Fatalf("archive spawned git\n%s", text)
	}
	for _, c := range d.Cards {
		if c.Column == "done" && (c.Error != "" || c.Approval.State != "none") {
			t.Fatal(c)
		}
	}
}

func TestStoppedRoundsAndAdmissionEvents(t *testing.T) {
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	admission := []history.Event{{Kind: history.Started, Round: 1}, {Kind: history.Queued, Round: 1}, {Kind: history.Resumed, Round: 2}, {Kind: history.Rerun, Round: 3}}
	for _, state := range []runs.State{runs.Queued, runs.Stopped, runs.Failed} {
		events := append(append([]history.Event{}, admission...), history.Event{Kind: history.Kind(state), Step: "build", Round: 3})
		if got := rounds(&runs.Run{State: state, Step: "build", Round: 3}, events); len(got) != 0 {
			t.Fatal("admission created rounds", got)
		}
	}
	for _, crashed := range []bool{false, true} {
		events := []history.Event{{Time: at, Kind: history.StepStarted, Step: "build", Round: 2}}
		r := &runs.Run{State: runs.Stopped, Step: "build", Round: 2, Since: at}
		if crashed {
			r.Reason = runs.Crashed
		} else {
			events = append(events, history.Event{Time: at, Kind: history.Stopped, Step: "build", Round: 2})
		}
		got := rounds(r, events)
		if len(got) != 1 || got[0].Result != "stopped" || got[0].Ended == "" {
			t.Fatal(got)
		}
	}
}

func TestMissingRunStepFallsBackToExistingColumn(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"old-step", "empty-step"}})
	for _, id := range []string{"old-step", "empty-step"} {
		wt := filepath.Join(filepath.Dir(root), "repo-worktrees", id)
		testenv.Git(t, root, "worktree", "add", "-qb", id, wt)
		st, err := statedir.Resolve(context.Background(), wt)
		if err != nil {
			t.Fatal(err)
		}
		step := "removed"
		if id == "empty-step" {
			step = ""
		}
		if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: id, State: runs.Blocked, Step: step}); err != nil {
			t.Fatal(err)
		}
	}
	d := read(t, root, "")
	for _, c := range d.Cards {
		if c.Column != "queued" || c.Error == "" || c.State != "blocked" {
			t.Fatal(c)
		}
		exists := false
		for _, col := range d.Columns {
			if col.ID == c.Column {
				exists = true
			}
		}
		if !exists {
			t.Fatal(c)
		}
	}
}

func TestBrokenArchiveAndDetailRead(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"good", "broken-detail"}})
	archived := filepath.Join(root, "openspec", "changes", "archive", "2026-11-12-broken-archive")
	testenv.WriteFile(t, filepath.Join(archived, "tuckerbox.toml"), "title = [\n")
	// A directory where proposal text belongs is a portable read error.
	proposal := filepath.Join(root, "openspec", "changes", "broken-detail", "proposal.md")
	if err := os.Remove(proposal); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(proposal, 0755); err != nil {
		t.Fatal(err)
	}
	d := read(t, root, "broken-detail")
	if len(d.Cards) != 3 || card(t, d, "good").Error != "" || card(t, d, "broken-archive").Error == "" || card(t, d, "broken-detail").Error == "" || d.Detail == nil {
		t.Fatal(d)
	}
}
