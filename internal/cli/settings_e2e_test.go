package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// add-change-settings 6.1, end to end in a scratch repository with approval
// required: the repository allows codex, tuckerbox set makes the change's
// build step codex, the change is approved and run to ready. Its history
// and its card both show the build step's agent as codex from the change,
// while the review step keeps the repository's agent.
func TestChangeSettingsEndToEnd(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.machineConfig(fakeCodex())

	// Allow codex in the repository config.
	f.setRepoConfig("\n[workflow]", "\n[overrides]\nagents = [\"codex\"]\n\n[workflow]")
	if res := f.tb("check"); res.code != 0 {
		t.Fatalf("check: exit %d: %s", res.code, res.stderr)
	}

	// Set the build step to codex, as the board's picker does, and commit
	// it so the run starts from it.
	if res := f.tb("set", "add-login", "step.build.agent=codex"); res.code != 0 {
		t.Fatalf("set: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	f.commit("build add-login with codex")

	// Approval is required: run refuses until the change is approved, and
	// the approval covers the agent override.
	if res := f.tb("run", "--foreground", "add-login"); res.code == 0 || !strings.Contains(res.stderr, "approv") {
		t.Fatalf("run before approval: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	f.noWorktree("add-login")
	f.mustApprove("add-login")
	if e := f.refEntryOf("add-login"); e.SettingsHash == "" {
		t.Errorf("approval has no settings hash: %+v", e)
	}

	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if s, _ := f.state("add-login"); s != "ready" {
		t.Fatalf("state %q, want ready", s)
	}

	// History: the build step ran codex, from the change.
	evs, _ := f.historyOf("add-login")
	build := stepStarted(t, evs, "build")
	if build.Agent != "codex" || build.Settings == nil ||
		build.Settings.Agent != (history.Setting{Value: "codex", Source: "change"}) {
		t.Errorf("build step_started: agent %q settings %+v", build.Agent, build.Settings)
	}
	review := stepStarted(t, evs, "review")
	if review.Agent != "fake" || review.Settings == nil || review.Settings.Agent.Source != "repo" {
		t.Errorf("review step_started: agent %q settings %+v", review.Agent, review.Settings)
	}
	doc := f.history("add-login")
	sawBuild := false
	for _, ev := range doc.Events {
		if ev.Step == "build" {
			sawBuild = true
			if ev.Agent != "codex" {
				t.Errorf("history --json: %s event for build has agent %q", ev.Kind, ev.Agent)
			}
		}
	}
	if !sawBuild {
		t.Errorf("history --json has no build events: %+v", doc.Events)
	}
	if text := f.tb("history", "add-login"); text.code != 0 || !strings.Contains(text.stdout, "codex") {
		t.Errorf("history: exit %d\n%s\n%s", text.code, text.stdout, text.stderr)
	}

	// The card: ready, and its build step shows codex from the change, on
	// the board and in the card detail with codex among the choices.
	for _, args := range [][]string{{"board", "--json"}, {"board", "--change", "add-login", "--json"}} {
		res := f.tb(args...)
		if res.code != 0 {
			t.Fatalf("%v: exit %d: %s", args, res.code, res.stderr)
		}
		var b board.Document
		if err := json.Unmarshal([]byte(res.stdout), &b); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, res.stdout)
		}
		if len(b.Cards) != 1 || b.Cards[0].Change != "add-login" {
			t.Fatalf("%v: cards %+v", args, b.Cards)
		}
		card := b.Cards[0]
		if card.State != "ready" || card.Approval.State != "approved" {
			t.Errorf("%v: card state %q approval %q", args, card.State, card.Approval.State)
		}
		var got *board.StepSettings
		for i := range card.Settings {
			if card.Settings[i].Step == "build" {
				got = &card.Settings[i]
			}
		}
		if got == nil || got.Agent != (board.Setting{Value: "codex", Source: "change"}) {
			t.Errorf("%v: build settings %+v in %+v", args, got, card.Settings)
		}
		if len(args) > 2 {
			if b.Detail == nil || len(b.Detail.Choices.Agents) != 1 || b.Detail.Choices.Agents[0] != "codex" {
				t.Errorf("detail choices: %+v", b.Detail)
			}
		}
	}
}
