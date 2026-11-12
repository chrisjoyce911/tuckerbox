package board

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func stepOf(t *testing.T, c Card, step string) StepSettings {
	t.Helper()
	for _, s := range c.Settings {
		if s.Step == step {
			return s
		}
	}
	t.Fatalf("card %s has no settings for step %s: %+v", c.Change, step, c.Settings)
	return StepSettings{}
}

// settings-board, scenario Card with an override: add-login overrides only
// [steps.build] agent = "codex"; its card's build entry has agent codex
// from change, and every other value comes from repo or default. Entries
// follow the workflow, and the card carries the change's machines.
func TestCardWithAnOverride(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login", "add-search"},
		Extra: "\n[overrides]\nagents = [\"codex\"]\n"})
	testenv.WriteFile(t, approval.DirOf(root, "add-login").SidecarPath(), "machines = [\"studio\", \"mini\"]\n\n[steps.build]\nagent = \"codex\"\n")

	d := read(t, root, "")
	c := card(t, d, "add-login")
	if steps := []string{c.Settings[0].Step, c.Settings[1].Step}; len(c.Settings) != 2 || !reflect.DeepEqual(steps, []string{"build", "review"}) {
		t.Fatalf("settings not in workflow order: %+v", c.Settings)
	}
	build := stepOf(t, c, "build")
	if build.Agent != (Setting{"codex", "change"}) || build.Started {
		t.Errorf("build entry %+v", build)
	}
	for _, s := range c.Settings {
		for key, v := range map[string]Setting{"agent": s.Agent, "model": s.Model, "effort": s.Effort, "permissions": s.Permissions} {
			if s.Step == "build" && key == "agent" {
				continue
			}
			if v.Source != "repo" && v.Source != "default" {
				t.Errorf("%s %s from %q, want repo or default", s.Step, key, v.Source)
			}
		}
	}
	if review := stepOf(t, c, "review"); review.Agent != (Setting{"fake", "repo"}) || review.Model != (Setting{"", "default"}) {
		t.Errorf("review entry %+v", review)
	}
	if !reflect.DeepEqual(c.Machines, []string{"studio", "mini"}) {
		t.Errorf("machines %v", c.Machines)
	}
	// A change without a sidecar: repository values, no machines.
	other := card(t, d, "add-search")
	if stepOf(t, other, "build").Agent != (Setting{"fake", "repo"}) || other.Machines == nil || len(other.Machines) != 0 {
		t.Errorf("plain card %+v %v", other.Settings, other.Machines)
	}
}

// settings-board, scenario Running step started before an edit: the build
// step started with model opus, then the repository config changed the
// model; the card's build entry still shows opus, the value it started
// with, and the review entry shows today's config.
func TestRunningStepStartedBeforeAnEdit(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	configPath := filepath.Join(root, config.RepoFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, configPath, strings.Replace(string(data), "role = \"builder\"\n", "role = \"builder\"\nmodel = \"sonnet\"\n", 1))

	wt := filepath.Join(filepath.Dir(root), "repo-worktrees", "add-login")
	testenv.Git(t, root, "worktree", "add", "-qb", "add-login", wt)
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	rec := history.Settings{
		Agent:       history.Setting{Value: "fake", Source: "repo"},
		Model:       history.Setting{Value: "opus", Source: "repo"},
		Effort:      history.Setting{Source: "default"},
		Permissions: history.Setting{Source: "default"},
	}
	run := &runs.Run{Change: "add-login", Repo: root, Worktree: wt, State: runs.Running, Step: "build", Round: 2, Since: time.Now(),
		StepSettings: &runs.StepSettings{Step: "build", Round: 2, Settings: rec}}
	if err := runs.WriteRun(st.RunFile(), run); err != nil {
		t.Fatal(err)
	}
	lock, err := runs.TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	c := card(t, read(t, root, ""), "add-login")
	build := stepOf(t, c, "build")
	if build.Model != (Setting{"opus", "repo"}) || !build.Started || build.Effort != (Setting{"", "default"}) {
		t.Errorf("running build entry %+v, want the model it started with", build)
	}
	if review := stepOf(t, c, "review"); review.Started || review.Model != (Setting{"", "default"}) {
		t.Errorf("review entry %+v", review)
	}

	// Stopped: the next step takes its settings afresh, so the card shows
	// today's config.
	lock.Release()
	run.State = runs.Stopped
	if err := runs.WriteRun(st.RunFile(), run); err != nil {
		t.Fatal(err)
	}
	if build := stepOf(t, card(t, read(t, root, ""), "add-login"), "build"); build.Model != (Setting{"sonnet", "repo"}) || build.Started {
		t.Errorf("stopped build entry %+v, want today's config", build)
	}
}

// settings-board, scenario Choices: [overrides] allows agents claude and
// codex and efforts medium and high; card detail's choices list both
// agents and both efforts and mark agent and permissions as voiding
// approval. With no [overrides], choices are empty.
func TestChoices(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"},
		Extra: "\n[overrides]\nagents = [\"claude\", \"codex\"]\nefforts = [\"medium\", \"high\"]\nmodels = { codex = [\"gpt-5.5\"] }\n"})
	d := read(t, root, "add-login")
	ch := d.Detail.Choices
	if !reflect.DeepEqual(ch.Agents, []string{"claude", "codex"}) || !reflect.DeepEqual(ch.Efforts, []string{"medium", "high"}) ||
		!reflect.DeepEqual(ch.Models, map[string][]string{"codex": {"gpt-5.5"}}) || len(ch.Permissions) != 0 {
		t.Errorf("choices %+v", ch)
	}
	if !reflect.DeepEqual(ch.VoidsApproval, []string{"agent", "permissions"}) {
		t.Errorf("voids_approval %v", ch.VoidsApproval)
	}
	if read(t, root, "").Detail != nil {
		t.Error("a board without --change has detail")
	}

	plain := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	if ch := read(t, plain, "add-login").Detail.Choices; !reflect.DeepEqual(ch, noChoices()) {
		t.Errorf("choices without [overrides] %+v", ch)
	}
}

// settings-board, scenarios Pick a model and Change agent on an approved
// card, as the next board read sees them: a model set by tuckerbox set
// shows from change and keeps the approval; an agent set on an approved
// card makes the approval stale and moves the card to Proposed.
func TestBoardAfterSet(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true,
		Extra: "\n[overrides]\nagents = [\"codex\"]\nmodels = { fake = [\"sonnet\"] }\n"})
	dir := approval.DirOf(root, "add-login")
	if _, err := approval.Approve(dir, "Reviewer", time.Now()); err != nil {
		t.Fatal(err)
	}
	if c := card(t, read(t, root, ""), "add-login"); c.Column != "approved" {
		t.Fatalf("card %+v", c)
	}
	data, err := os.ReadFile(dir.SidecarPath())
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, dir.SidecarPath(), string(data)+"\n[steps.review]\nmodel = \"sonnet\"\n")
	c := card(t, read(t, root, ""), "add-login")
	if c.Column != "approved" || c.Approval.State != "approved" || stepOf(t, c, "review").Model != (Setting{"sonnet", "change"}) {
		t.Errorf("after a model pick: %s %s %+v", c.Column, c.Approval.State, stepOf(t, c, "review"))
	}
	testenv.WriteFile(t, dir.SidecarPath(), string(data)+"\n[steps.review]\nmodel = \"sonnet\"\n\n[steps.build]\nagent = \"codex\"\n")
	c = card(t, read(t, root, ""), "add-login")
	if c.Column != "proposed" || c.Approval.State != "stale" || stepOf(t, c, "build").Agent != (Setting{"codex", "change"}) {
		t.Errorf("after an agent pick: %s %s %+v", c.Column, c.Approval.State, stepOf(t, c, "build"))
	}
}
