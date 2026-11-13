package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

var testTools *testenv.Tools

func TestMain(m *testing.M) {
	var err error
	testTools, err = testenv.Build(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sshName := "ssh"
	if runtime.GOOS == "windows" {
		sshName += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(testTools.Bin, sshName), "./testdata/fakessh")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if data, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building fake ssh: %v: %s", err, data)
		testTools.Remove()
		os.Exit(1)
	}
	code := m.Run()
	testTools.Remove()
	os.Exit(code)
}
func read(t *testing.T, root, change string) *Document {
	t.Helper()
	d, err := Read(context.Background(), root, change, config.Env{})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func card(t *testing.T, d *Document, id string) Card {
	t.Helper()
	for _, c := range d.Cards {
		if c.Change == id {
			return c
		}
	}
	t.Fatalf("missing card %s", id)
	return Card{}
}
func TestNoRunsAndApproval(t *testing.T) {
	for _, layout := range []string{"compact", "steps"} {
		t.Run(layout, func(t *testing.T) {
			testTools.Setup(t)
			root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login-page", "add-search", "add-users"}, RequireApproval: true})
			configPath := filepath.Join(root, config.RepoFile)
			original, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise the default compact layout and the explicit steps layout.
			if layout == "steps" {
				testenv.WriteFile(t, configPath, string(original)+"\n[board]\ncolumns = 'steps'\n")
			}
			d := read(t, root, "")
			if d.Repo != root || len(d.Cards) != 3 {
				t.Fatalf("document %+v", d)
			}
			for _, c := range d.Cards {
				if c.Column != "proposed" || c.Approval.State != "none" || !c.Approval.Committed || c.EditFolder != root || c.Tasks.Total != 1 {
					t.Errorf("card %+v", c)
				}
			}
			if c := card(t, d, "add-login-page"); c.Title != "add login page" {
				t.Fatal(c)
			}
			dir := approval.DirOf(root, "add-login-page")
			testenv.WriteFile(t, dir.SidecarPath(), "title = 'Login page'\n")
			if _, err := approval.Approve(dir, "Reviewer", time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			c := card(t, read(t, root, ""), "add-login-page")
			if c.Column != "approved" || c.Title != "Login page" || c.Approval.Committed || c.Approval.At != timestamp(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("approval %+v", c)
			}
			testenv.Git(t, root, "add", "-A")
			testenv.Git(t, root, "commit", "-qm", "approve")
			if !card(t, read(t, root, ""), "add-login-page").Approval.Committed {
				t.Fatal("committed approval marked dirty")
			}
			testenv.WriteFile(t, filepath.Join(string(dir), "proposal.md"), "edited proposal\n")
			c = card(t, read(t, root, ""), "add-login-page")
			if c.Column != "proposed" || c.Approval.State != "stale" || c.Approval.Committed {
				t.Fatal(c)
			}
		})
	}
}
func TestColumns(t *testing.T) {
	for _, layout := range []config.BoardColumns{"", config.BoardCompact, config.BoardSteps} {
		for _, required := range []bool{true, false} {
			cfg := &config.Config{BoardColumns: layout, RequireApproval: required, Steps: []config.Step{{ID: "plan"}, {ID: "build"}, {ID: "review"}}}
			want := []string{"proposed"}
			if required {
				want = append(want, "approved")
			}
			if layout == config.BoardSteps {
				want = append(want, "queued", "step:plan", "step:build", "step:review")
			} else {
				want = append(want, "in_progress")
			}
			want = append(want, "ready", "done")
			var ids []string
			for _, c := range columns(cfg) {
				ids = append(ids, c.ID)
				if c.ID == "in_progress" && c.Name != "In progress" {
					t.Fatal(c)
				}
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("%s approval=%v: %v, want %v", layout, required, ids, want)
			}
		}
	}
}
func TestApprovalOff(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	if _, err := approval.Approve(approval.DirOf(root, "add-login"), "Reviewer", time.Now()); err != nil {
		t.Fatal(err)
	}
	d := read(t, root, "")
	if d.RequireApproval || card(t, d, "add-login").Column != "proposed" {
		t.Fatal(d)
	}
	for _, c := range d.Columns {
		if c.ID == "approved" {
			t.Fatal(c)
		}
	}
}
func TestNotRepository(t *testing.T) {
	testTools.Setup(t)
	for _, root := range []string{t.TempDir(), testTools.NewRepo(t, testenv.RepoOptions{NoConfig: true})} {
		_, err := Read(context.Background(), root, "", config.Env{})
		var be *Error
		if !errors.As(err, &be) || be.Code != "not_a_repository" {
			t.Fatalf("error %v", err)
		}
	}
}

func TestRunsDependenciesAndDetail(t *testing.T) {
	testTools.Setup(t)
	ids := []string{"add-login", "add-users", "waiting", "ready-change", "failed-change", "stopped-change"}
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: ids, RequireApproval: true})
	configPath := filepath.Join(root, config.RepoFile)
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, configPath, string(original)+"\n[board]\ncolumns = 'steps'\n")
	wt := filepath.Join(filepath.Dir(root), "repo-worktrees", "add-login")
	testenv.Git(t, root, "worktree", "add", "-qb", "tuckerbox/add-login", wt)
	// The board must read the worktree, including its uncommitted edits.
	dir := string(approval.DirOf(wt, "add-login"))
	testenv.WriteFile(t, filepath.Join(dir, "tasks.md"), "# Tasks\r\n## 2. Sign in\r\n- [x] 2.1 Check password\r\n- [ ] 2.2 Hide errors\r\n")
	testenv.WriteFile(t, filepath.Join(dir, "specs", "login", "spec.md"), "login delta\n")
	testenv.WriteFile(t, filepath.Join(dir, "tuckerbox.toml"), "depends_on = ['add-users', 'old-change', 'unknown']\n")
	at := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	finding := agent.Finding{Severity: "high", File: "login.go", Detail: "password logged"}
	run := &runs.Run{Change: "add-login", Repo: root, Worktree: wt, Branch: "tuckerbox/add-login", State: runs.Blocked, Reason: "review limit reached", Step: "review", Round: 2, Since: at, History: []runs.Entry{
		{Round: 1, Step: "review", Status: agent.Rejected, Summary: "fix it", Findings: []agent.Finding{finding}, Ended: at.Add(-time.Hour)},
		{Round: 2, Step: "review", Status: agent.Rejected, Summary: "still wrong", Findings: []agent.Finding{finding}, Ended: at},
	}}
	if err := runs.WriteRun(st.RunFile(), run); err != nil {
		t.Fatal(err)
	}
	for _, e := range []history.Event{
		{Time: at.Add(-time.Hour), Kind: history.Rejected, Round: 1, Step: "review", Summary: "fix it", Findings: []agent.Finding{finding}},
		{Time: at.Add(-time.Minute), Kind: history.StepStarted, Round: 2, Step: "build"},
		{Time: at, Kind: history.StepFinished, Round: 2, Step: "review", Status: agent.Rejected, Summary: "still wrong", Findings: []agent.Finding{finding}},
		{Time: at, Kind: history.Blocked, Round: 2, Step: "review", Reason: "review limit reached"},
	} {
		if err := history.Append(history.File(st), e); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		id    string
		state runs.State
		step  string
	}{{"add-users", runs.Running, "build"}, {"waiting", runs.Queued, "build"}, {"ready-change", runs.Ready, ""}, {"failed-change", runs.Failed, "build"}, {"stopped-change", runs.Stopped, "review"}} {
		path := filepath.Join(filepath.Dir(root), "repo-worktrees", item.id)
		testenv.Git(t, root, "worktree", "add", "-qb", item.id, path)
		s, err := statedir.Resolve(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := runs.WriteRun(s.RunFile(), &runs.Run{Change: item.id, Repo: root, Worktree: path, State: item.state, Step: item.step, Round: 1, Since: at}); err != nil {
			t.Fatal(err)
		}
		if item.state == runs.Running || item.state == runs.Queued {
			lock, err := runs.TryChangeLock(s.RunLock())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { lock.Release() })
		}
	}
	testenv.WriteFile(t, filepath.Join(root, "openspec", "changes", "archive", "2026-01-01-old-change", "proposal.md"), "old\n")
	d := read(t, root, "add-login")
	c := card(t, d, "add-login")
	if c.Column != "step:review" || c.State != "blocked" || c.Reason != "review limit reached" || c.Agent != "fake" || c.EditFolder != wt || c.Tasks.Done != 1 || c.Tasks.Total != 2 {
		t.Fatalf("card %+v", c)
	}
	want := []Dependency{{"add-users", "step:build"}, {"old-change", "done"}, {"unknown", ""}}
	if !reflect.DeepEqual(c.DependsOn, want) {
		t.Fatal(c.DependsOn)
	}
	for id, want := range map[string]string{"waiting": "queued", "ready-change": "ready", "failed-change": "step:build", "stopped-change": "step:review"} {
		if card(t, d, id).Column != want {
			t.Fatal(id, card(t, d, id))
		}
	}
	detail := d.Detail
	if len(detail.Rounds) != 2 || detail.Rounds[0].Result != "rejected" || detail.Rounds[1].Result != "blocked" || len(detail.Rounds[0].Findings) != 1 || len(detail.History) != 4 || detail.History[2].Result != "rejected" {
		t.Fatalf("detail %+v", detail)
	}
	if len(detail.Tasks) != 2 || detail.Tasks[0].Number != "2.1" || !detail.Tasks[0].Checked || detail.Tasks[0].Group != "2. Sign in" || len(detail.Specs) != 1 || detail.Specs[0].Capability != "login" {
		t.Fatal(detail)
	}
	assertShape(t, d, "detail.json")
	// Omit board.columns: compact is the default, and dependencies use the same ids.
	testenv.WriteFile(t, configPath, string(original))
	d = read(t, root, "add-login")
	for id, badge := range map[string]string{"add-login": "review", "add-users": "build", "waiting": "queued", "failed-change": "build", "stopped-change": "review"} {
		c := card(t, d, id)
		if c.Column != "in_progress" || c.Badge != badge {
			t.Fatal(c)
		}
	}
	if c := card(t, d, "ready-change"); c.Column != "ready" || c.Badge != "" {
		t.Fatal(c)
	}
	if c := card(t, d, "add-login"); c.DependsOn[0].Column != "in_progress" {
		t.Fatal(c)
	}
	if !reflect.DeepEqual(d.Steps, []string{"build", "review"}) {
		t.Fatal(d.Steps)
	}
	assertShape(t, d, "detail.json")
}

func TestArchiveLimitAndDependency(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"active"}})
	for i := 1; i <= 22; i++ {
		p := filepath.Join(root, "openspec", "changes", "archive", fmt.Sprintf("2026-10-%02d-old-%02d", i, i))
		testenv.WriteFile(t, filepath.Join(p, "tasks.md"), "- [x] 1.1 Done\n")
	}
	testenv.WriteFile(t, approval.DirOf(root, "active").SidecarPath(), "depends_on = ['old-01']\n")
	d := read(t, root, "")
	if len(d.Cards) != 21 || d.Cards[1].Archived != "2026-10-22" || d.Cards[20].Archived != "2026-10-03" {
		t.Fatal(d.Cards)
	}
	if card(t, d, "active").DependsOn[0].Column != "done" {
		t.Fatal(d.Cards[0])
	}
	assertShape(t, d, "board.json")
	archived := read(t, root, "old-22")
	if archived.Detail == nil || len(archived.Detail.Tasks) != 1 || archived.Cards[1].EditFolder != "" {
		t.Fatal(archived)
	}
}

// The fixtures are the published schema snapshots. Require exact field/type
// unions (including optional fields); also decode/re-encode without loss to
// catch fixture additions not yet represented by the Go contract.
func shape(v any, prefix string, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		out[prefix] = "object"
		for k, v := range x {
			shape(v, prefix+"."+k, out)
		}
	case []any:
		out[prefix] = "array"
		for _, v := range x {
			shape(v, prefix+"[]", out)
		}
	case string:
		out[prefix] = "string"
	case bool:
		out[prefix] = "bool"
	case float64:
		out[prefix] = "number"
	default:
		out[prefix] = "null"
	}
}
func assertShape(t *testing.T, d *Document, fixture string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "herdr", "mods", "tuckerbox-board", "fixtures", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	normalized, _ := json.Marshal(snapshot)
	var original, roundtrip any
	json.Unmarshal(data, &original)
	json.Unmarshal(normalized, &roundtrip)
	if !reflect.DeepEqual(original, roundtrip) {
		t.Fatal("fixture round-trip lost fields")
	}
	// Include optional event variants from the fixture snapshot; the generated
	// document must supply all mandatory fields with the same primitive types.
	generated, _ := json.Marshal(d)
	var actual any
	json.Unmarshal(generated, &actual)
	want, got := map[string]string{}, map[string]string{}
	shape(original, "", want)
	shape(actual, "", got)
	// priority is a number or null, and position is absent on archived cards.
	optional := map[string]bool{".cards[].archived": true, ".cards[].error": true, ".columns[].step": true,
		".cards[].priority": true, ".cards[].position": true,
		".cards[].scope": true, ".cards[].scope.spec": true, ".cards[].scope.tasks": true, ".cards[].scope.tasks[]": true,
		".cards[].needs_you": true, ".autopilot.pid": true, ".autopilot.started": true, ".autopilot.machine": true, ".autopilot.terminal": true}
	for _, k := range []string{"mr", "mr.link", "mr.number", "mr.state", "ci", "ci.state", "ci.commit", "ship_round", "max_rounds"} {
		optional[".cards[]."+k] = true
	}
	// activity is only on running, queued and stopped cards; its task and
	// last tick may be null, and only a terminal agent has a terminal.
	for _, k := range []string{"", ".task", ".task.number", ".task.text", ".last_tick", ".last_tick.number", ".last_tick.at", ".last_tick.ticked", ".last_tick.step", ".last_tick.round", ".terminal", ".terminal.address", ".terminal.name", ".terminal.machine"} {
		optional[".cards[].activity"+k] = true
	}
	for _, k := range []string{"result", "to", "summary", "findings", "reason", "scope", "old_scope", "commit", "branch", "link", "number", "method", "ship_round", "ci", "jobs", "threads", "task"} {
		optional[".detail.history[]."+k] = true
	}
	// A card may have no machines and a step no recorded settings; the
	// allow-list's entries depend on the repository.
	optional[".cards[].machines[]"] = true
	// modes-board: fleet and team mode only.
	for _, k := range []string{".fleet", ".fleet.max_runs", ".fleet.claims", ".claims_read", ".claims_read.source", ".claims_read.at", ".claims_read.stale", ".claims_read.error",
		".cards[].claim", ".cards[].claim.machine", ".cards[].claim.state", ".cards[].claim.epoch", ".cards[].claim.renewed_at", ".cards[].claim.login", ".cards[].claim.run"} {
		optional[k] = true
	}
	optional[".cards[].settings[].started"] = true
	for _, k := range []string{"agents[]", "efforts[]", "permissions[]", "voids_approval[]"} {
		optional[".detail.choices."+k] = true
	}
	for k, v := range want {
		if optional[k] || strings.HasPrefix(k, ".detail.history[].findings[]") || strings.HasPrefix(k, ".detail.choices.models.") {
			continue
		}
		// A board with no running, queued or stopped card has no activity,
		// and a run without ticks none in its detail.
		if strings.HasPrefix(k, ".cards[].activity.") && got[".cards[].activity"] == "" ||
			strings.HasPrefix(k, ".detail.ticks[]") && got[".detail.ticks[]"] == "" {
			continue
		}
		if got[k] != v {
			t.Errorf("fixture %s shape %s: got %q, want %q", fixture, k, got[k], v)
		}
	}
	for k, v := range got {
		if strings.HasPrefix(k, ".cards[].activity.") && want[".cards[].activity"] == "" {
			continue
		}
		if !optional[k] && !strings.HasPrefix(k, ".detail.choices.models.") && want[k] != v {
			t.Errorf("unexpected shape %s: %s", k, v)
		}
	}
}
func TestFixtureShapes(t *testing.T) {
	for _, name := range []string{"board.json", "detail.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "herdr", "mods", "tuckerbox-board", "fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		var doc Document
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		assertShape(t, &doc, name)
	}
}

func TestRepoKey(t *testing.T) {
	for _, remote := range []string{"git@GH:Owner/repo.git", "ssh://git@GH:2222/Owner/repo.git", "https://github.com/Owner/repo.git/", "git://github.com/Owner/repo"} {
		key := repoKey(context.Background(), remote, func(_ context.Context, host string) (string, error) {
			if host != "GH" {
				t.Fatal(host)
			}
			return "hostname github.com\n", nil
		})
		if key != "github.com/Owner/repo" {
			t.Fatal(remote, key)
		}
	}
	if key := repoKey(context.Background(), "git@alias:owner/repo.git", func(context.Context, string) (string, error) { return "", errors.New("ssh missing") }); key != "alias/owner/repo" {
		t.Fatal(key)
	}
	if RepoKey(context.Background(), "") != "" {
		t.Fatal("empty remote")
	}
}

func TestCommittedChanges(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	dir := approval.DirOf(root, "add-login")
	if _, err := approval.Approve(dir, "Reviewer", time.Now()); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, root, "add", "-A")
	testenv.Git(t, root, "commit", "-qm", "approve")
	check := func(want bool) {
		t.Helper()
		got, err := committed(context.Background(), string(dir))
		if err != nil || got != want {
			t.Fatalf("committed=%v err=%v, want %v", got, err, want)
		}
	}
	check(true)
	old, err := os.ReadFile(dir.SidecarPath())
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, dir.SidecarPath(), "title = 'New title'\ndepends_on = ['other']\n"+string(old))
	check(true)
	// Tick-only changes keep the approval hash but are still uncommitted.
	tasks := filepath.Join(string(dir), "tasks.md")
	testenv.WriteFile(t, tasks, "# Tasks\n\n- [x] 1.1 Do it\n")
	if st, err := approval.Judge(dir); err != nil || st.State != approval.Approved {
		t.Fatal(st, err)
	}
	check(false)
	testenv.Git(t, root, "add", tasks)
	check(false)
	testenv.Git(t, root, "restore", "--staged", tasks)
	testenv.Git(t, root, "restore", tasks)
	check(true)
	if err := os.Remove(filepath.Join(string(dir), "proposal.md")); err != nil {
		t.Fatal(err)
	}
	check(false)
	testenv.Git(t, root, "restore", ".")
	testenv.WriteFile(t, filepath.Join(string(dir), "specs", "new", "spec.md"), "new delta\n")
	check(false)
}

func TestRoundsWithoutHistoryAndRunningRound(t *testing.T) {
	at := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	finding := agent.Finding{Severity: "high", Detail: "fix"}
	r := &runs.Run{State: runs.Running, Step: "build", Round: 2, History: []runs.Entry{{Step: "review", Round: 1, Status: agent.Rejected, Findings: []agent.Finding{finding}, Ended: at}}}
	got := rounds(r, nil)
	if len(got) != 2 || got[0].Result != "rejected" || len(got[0].Findings) != 1 || got[1].Result != "" || got[1].Step != "build" || got[1].Findings == nil {
		t.Fatal(got)
	}
}

func TestCrashedRun(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	wt := filepath.Join(filepath.Dir(root), "repo-worktrees", "add-login")
	testenv.Git(t, root, "worktree", "add", "-qb", "add-login", wt)
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: "add-login", State: runs.Running, Step: "build", Round: 1, Worktree: wt}); err != nil {
		t.Fatal(err)
	}
	c := card(t, read(t, root, ""), "add-login")
	if c.State != "stopped" || c.Reason != runs.Crashed || c.Column != "in_progress" || c.Badge != "build" {
		t.Fatal(c)
	}
	stored, err := runs.ReadRun(st.RunFile())
	if err != nil || stored.State != runs.Running {
		t.Fatal("board rewrote state", stored, err)
	}
}

func TestTaskParsing(t *testing.T) {
	text := "## 1. Group\r\n* [X] 1.1.1 Nested\r\n+ [ ] 1.2\r\n````markdown\n- [x] 9.1 Example\n```\n- [ ] 9.2 Still fenced\n````\n- [x] 2.1 Real\n- [ ] Unnumbered\n"
	tasks := parseTasks(text)
	if len(tasks) != 4 || tasks[0].Number != "1.1.1" || !tasks[0].Checked || tasks[1].Text != "" || tasks[2].Number != "2.1" {
		t.Fatal(tasks)
	}
}

func TestStagedApprovalDiffersFromWorkingCopy(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	dir := approval.DirOf(root, "add-login")
	if _, err := approval.Approve(dir, "First", time.Now()); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, root, "add", "-A")
	testenv.Git(t, root, "commit", "-qm", "approve")
	original, err := os.ReadFile(dir.SidecarPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := approval.Approve(dir, "Second", time.Now()); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, root, "add", dir.SidecarPath())
	testenv.WriteFile(t, dir.SidecarPath(), string(original))
	if ok, err := committed(context.Background(), string(dir)); err != nil || ok {
		t.Fatal(ok, err)
	}
}

// Scenario: Board order. Two Approved cards, A at 2000 and B at 1000: B is
// listed before A, with positions in priority order; the priorities come
// from the checkout. Ready keeps its order.
func TestBoardOrder(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"a", "b", "c"}, RequireApproval: true})
	for _, id := range []string{"a", "b"} {
		if _, err := approval.Approve(approval.DirOf(root, id), "T", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for id, v := range map[string]string{"a": "2000", "b": "1000"} {
		p := approval.DirOf(root, id).SidecarPath()
		data, _ := os.ReadFile(p)
		testenv.WriteFile(t, p, "priority = "+v+"\n"+string(data))
	}
	d := read(t, root, "")
	var approved []string
	for _, c := range d.Cards {
		if c.Column == "approved" {
			approved = append(approved, c.Change)
		}
	}
	if strings.Join(approved, ",") != "b,a" {
		t.Errorf("approved column %v", approved)
	}
	a, b, c := card(t, d, "a"), card(t, d, "b"), card(t, d, "c")
	if b.Position != 1 || a.Position != 2 || c.Position != 3 || *b.Priority != 1000 || *a.Priority != 2000 || c.Priority != nil {
		t.Errorf("positions a %d b %d c %d", a.Position, b.Position, c.Position)
	}
	data, _ := json.Marshal(c)
	if !strings.Contains(string(data), `"priority":null`) || !strings.Contains(string(data), `"position":3`) {
		t.Errorf("card json %s", data)
	}
}

func TestSetPrioritiesKeepsUnsortedColumns(t *testing.T) {
	cards := []Card{
		{Change: "r2", Column: "ready"}, {Change: "p2", Column: "proposed"}, {Change: "r1", Column: "ready"},
		{Change: "p1", Column: "proposed"}, {Change: "s", Column: "step:build"}, {Change: "x", Column: "proposed"},
	}
	root := t.TempDir()
	for id, v := range map[string]string{"p1": "1", "p2": "2", "s": "3", "r1": "4", "r2": "5"} {
		testenv.WriteFile(t, filepath.Join(root, "openspec", "changes", id, "tuckerbox.toml"), "priority = "+v+"\n")
	}
	setPriorities(cards, root)
	var got []string
	for _, c := range cards {
		got = append(got, c.Change)
	}
	// Sorted cards fill the sorted slots in priority order (x has no folder,
	// so no position, and goes last); ready cards stay where they were.
	if strings.Join(got, ",") != "r2,p1,r1,p2,s,x" {
		t.Errorf("cards %v", got)
	}
}

// approval-publish: the board reads approvals from the approvals ref, and
// an entry there wins over the sidecar.
func TestBoardReadsApprovalsRef(t *testing.T) {
	ctx := context.Background()
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login", "add-users"}, RequireApproval: true})
	dir := approval.DirOf(root, "add-login")
	hash, err := approval.Hash(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := approval.Store{Dir: root}
	if err := s.Record(ctx, "add-login", approval.Entry{State: approval.EntryApproved, Hash: hash, By: "Owner", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := approval.Approve(approval.DirOf(root, "add-users"), "Old", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, "add-users", approval.Entry{State: approval.EntryWithdrawn, By: "Owner", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	doc := read(t, root, "")
	got := map[string]Approval{}
	for _, c := range doc.Cards {
		got[c.Change] = c.Approval
	}
	if got["add-login"].State != "approved" || got["add-login"].By != "Owner" {
		t.Errorf("add-login approval = %+v", got["add-login"])
	}
	if got["add-users"].State != "none" {
		t.Errorf("add-users after withdrawal = %+v", got["add-users"])
	}
}

// approval-publish: Card after publishing: published is false while this
// machine's approval isn't on the remote, and true once a board read could
// publish it.
func TestBoardPublishedState(t *testing.T) {
	ctx := context.Background()
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	bare := filepath.Join(t.TempDir(), "origin.git")
	testenv.Git(t, filepath.Dir(bare), "init", "-q", "--bare", "-b", "main", bare)
	testenv.Git(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "gone.git"))
	hash, err := approval.Hash(approval.DirOf(root, "add-login"))
	if err != nil {
		t.Fatal(err)
	}
	err = approval.Store{Dir: root}.Record(ctx, "add-login", approval.Entry{State: approval.EntryApproved, Hash: hash, By: "Owner", At: time.Now()})
	if !errors.Is(err, approval.ErrOffline) {
		t.Fatalf("Record = %v, want offline", err)
	}
	if a := card(t, read(t, root, ""), "add-login").Approval; a.State != "approved" || a.Published {
		t.Fatalf("offline card approval = %+v", a)
	}
	testenv.Git(t, root, "remote", "set-url", "origin", bare)
	if a := card(t, read(t, root, ""), "add-login").Approval; a.State != "approved" || !a.Published {
		t.Fatalf("after publishing: %+v", a)
	}
	if es, _ := (approval.Store{Dir: bare}).Entries(ctx); es["add-login"].Hash != hash {
		t.Fatalf("remote entries = %+v", es)
	}
}
