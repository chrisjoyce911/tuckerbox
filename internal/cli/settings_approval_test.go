package cli

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// refEntryOf reads id's entry on the approvals ref.
func (f *cliFixture) refEntryOf(id string) approval.Entry {
	f.t.Helper()
	es, err := (approval.Store{Dir: f.repo}).Entries(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return es[id]
}

// change-settings, scenario Agent changed after approval: approve records
// the settings hash; tuckerbox set step.build.agent=codex makes the
// approval stale and tuckerbox run refuses, naming the override, with no
// worktree made. Approving again covers it; unapprove withdraws it.
func TestAgentChangedAfterApprovalRefusesRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true,
		Extra: "\n[overrides]\nagents = [\"codex\"]\n"})
	f.mustApprove("add-login")
	if e := f.refEntryOf("add-login"); e.SettingsHash != approval.SettingsHash(nil) || len(e.Settings) != 0 {
		t.Fatalf("entry %+v", e)
	}
	if res := f.tb("set", "add-login", "step.build.agent=codex"); res.code != 0 {
		t.Fatalf("set: %d %s", res.code, res.stderr)
	}
	f.commit("build with codex")
	st, err := currentApproval(context.Background(), f.repo, "add-login")
	if err != nil || st.State != approval.Stale || st.TextStale || !slices.Equal(st.SettingsChanged, []string{"step.build.agent"}) {
		t.Fatalf("after set: %+v %v", st, err)
	}
	res := f.tb("run", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "approval is stale on the base branch main: its step.build.agent changed") ||
		!strings.Contains(res.stderr, "tuckerbox approve add-login") {
		t.Errorf("run: %d %s", res.code, res.stderr)
	}
	f.noWorktree("add-login")

	res = f.tb("approve", "add-login")
	if res.code != 0 || !strings.Contains(res.stdout, "step.build.agent=codex") {
		t.Fatalf("approve again: %d %s %s", res.code, res.stdout, res.stderr)
	}
	e := f.refEntryOf("add-login")
	if !slices.Equal(e.Settings, []string{"step.build.agent=codex"}) || e.SettingsHash != approval.SettingsHash(e.Settings) {
		t.Errorf("entry %+v", e)
	}
	if f.approvalState(f.repo, "add-login") != approval.Approved {
		t.Error("not approved after approving again")
	}
	if res := f.tb("unapprove", "add-login"); res.code != 0 {
		t.Fatalf("unapprove: %d %s", res.code, res.stderr)
	}
	if e := f.refEntryOf("add-login"); e.State != approval.EntryWithdrawn || e.SettingsHash != "" || e.Settings != nil {
		t.Errorf("withdrawn entry %+v", e)
	}
	if f.approvalState(f.repo, "add-login") != approval.None {
		t.Error("not none after unapprove")
	}
}

// change-settings, scenario Old approval without overrides: a committed
// [approval] written before settings were hashed, on a change with no step
// overrides, is still approved and the run starts.
func TestOldApprovalWithoutOverridesRuns(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	hash, err := approval.Hash(approval.DirOf(f.repo, "add-login"))
	if err != nil {
		t.Fatal(err)
	}
	f.setSidecar("add-login", "[approval]\nhash = \""+hash+"\"\nby = \"Old\"\nat = 2026-01-01T09:00:00+10:00\n")
	if f.approvalState(f.repo, "add-login") != approval.Approved {
		t.Fatal("old approval not approved")
	}
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}

// change-settings, scenario Model changed after approval (and effort and
// machines): the approval stays approved and the run starts.
func TestModelChangedAfterApprovalKeepsIt(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true,
		Extra: "\n[overrides]\nmodels = { fake = [\"sonnet\"] }\nefforts = [\"high\"]\n"})
	f.mustApprove("add-login")
	for _, kv := range []string{"step.build.model=sonnet", "step.review.effort=high", "machines=studio,mini"} {
		if res := f.tb("set", "add-login", kv); res.code != 0 {
			t.Fatalf("set %s: %d %s", kv, res.code, res.stderr)
		}
		if s := f.approvalState(f.repo, "add-login"); s != approval.Approved {
			t.Errorf("after %s: %s", kv, s)
		}
	}
	f.commit("model, effort and machines")
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}

// change-settings, scenario Agent changed on a stopped run: an approved
// change's run is stopped, the review agent is set to codex in its
// worktree, and resume refuses, naming step.review.agent and saying to
// approve again; rerun refuses too and the run stays stopped. Setting it
// back lets the run resume.
func TestAgentChangedOnStoppedRun(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true,
		Extra: "\n[overrides]\nagents = [\"codex\"]\n"})
	f.mustApprove("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"review:1": {WaitFor: f.gate("never")}})
	if res := f.tb("run", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "running", "review")
	if res := f.tb("stop", "add-login"); res.code != 0 {
		t.Fatalf("stop: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "stopped", "review")
	if res := f.tb("set", "add-login", "step.review.agent=codex"); res.code != 0 || !strings.Contains(res.stdout, "change's worktree") {
		t.Fatalf("set: %d %s %s", res.code, res.stdout, res.stderr)
	}

	for _, args := range [][]string{
		{"resume", "add-login"},
		{"resume", "--foreground", "add-login"},
		{"rerun", "add-login", "review"},
	} {
		res := f.tb(args...)
		if res.code == 0 || !strings.Contains(res.stderr, "step.review.agent changed since Test approved it") ||
			!strings.Contains(res.stderr, "tuckerbox approve add-login again") {
			t.Errorf("%v: %d %s", args, res.code, res.stderr)
		}
		if s, step := f.state("add-login"); s != "stopped" || step != "review" {
			t.Errorf("%v changed the run: %s %s", args, s, step)
		}
	}

	if res := f.tb("set", "add-login", "--unset", "step.review.agent"); res.code != 0 {
		t.Fatalf("unset: %d %s", res.code, res.stderr)
	}
	f.env.SetPlan(t, map[string]testenv.Action{})
	if res := f.tb("resume", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("resume after setting it back: %d %s %s", res.code, res.stdout, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}

// change-settings, scenario Plan step rewrote the design: a step edited the
// design, making the text hash stale, and the run then blocked; resume
// continues the run, as the settings hash still matches.
func TestPlanStepRewroteDesign(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.mustApprove("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{
		"build:1":  {Edit: "design.md"},
		"review:1": {Status: "blocked", Summary: "needs a person"},
	})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	f.waitState("add-login", "blocked", "")
	st, err := currentApproval(context.Background(), f.worktreeOf("add-login"), "add-login")
	if err != nil || st.State != approval.Stale || !st.TextStale || st.SettingsChanged != nil {
		t.Fatalf("after the design edit: %+v %v", st, err)
	}
	f.env.SetPlan(t, map[string]testenv.Action{})
	if res := f.tb("resume", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("resume: %d %s %s", res.code, res.stdout, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}
