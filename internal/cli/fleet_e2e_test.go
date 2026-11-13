package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv/fleetforge"
)

func fleetCrashCLI(t *testing.T) string {
	t.Helper()
	name := "fleetcli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", path, "./testdata/fleetcli")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building fleet CLI shim: %v\n%s", err, out)
	}
	return path
}
func fleetCrashTB(t *testing.T, m *otherMachine, binary string, args ...string) cmdResult {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = m.repo
	cmd.Env = append(os.Environ(), m.env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return cmdResult{out.String(), errOut.String(), code}
}
func fleetCrashMust(t *testing.T, m *otherMachine, binary string, args ...string) cmdResult {
	t.Helper()
	r := fleetCrashTB(t, m, binary, args...)
	if r.code != 0 {
		t.Fatalf("%s %v: exit%d\n%s\n%s", m.name, args, r.code, r.stdout, r.stderr)
	}
	return r
}
func fleetCrashRun(t *testing.T, m *otherMachine, change string) *runs.Run {
	t.Helper()
	for _, row := range m.status().Runs {
		if row.Change == change {
			st, err := statedir.Resolve(context.Background(), row.Worktree)
			if err != nil {
				t.Fatal(err)
			}
			r, err := runs.ReadRun(st.RunFile())
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatalf("no %s run on %s", change, m.name)
	return nil
}
func fleetCrashKill(t *testing.T, r *runs.Run) {
	t.Helper()
	for _, pid := range []int{r.Pid, r.AgentPid} {
		if pid > 0 {
			p, err := os.FindProcess(pid)
			if err == nil {
				_ = p.Kill()
			}
		}
	}
	st, err := statedir.Resolve(context.Background(), r.Worktree)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "killed engine's change lock released", func() bool { return !runs.Active(st.RunLock()) })
	saved, err := runs.ReadRun(st.RunFile())
	if err != nil || saved.State != r.State {
		t.Fatalf("crash recorded a final state: %+v %v", saved, err)
	}
}
func fleetCrashSnapshot(t *testing.T, r *runs.Run) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), r.Change+".json")
	if err := runs.WriteRun(p, r); err != nil {
		t.Fatal(err)
	}
	return p
}

// Task 7.1: the real dispatcher starts two real detached engines over fake SSH.
// Each worker has its own clone/config/state directory and both use one bare
// remote. Killing engines (not stop) leaves active claims; the test clock ages
// them without waiting ten minutes. The restarted machines swap holders, then
// stale saved attempts try every fenced shared effect.
func TestFleetEndToEndCrashDuringStepsAndCI(t *testing.T) {
	const buildChange = "build-crash"
	const ciChange = "ci-crash"
	ctx := context.Background()
	f := newFleet(t, []string{buildChange, ciChange}, "", "", "alpha", "beta")
	t.Setenv(config.TestForgeURLEnv, "https://github.com/example/fleet.git")
	cfgPath := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	cfg := strings.Replace(readFile(t, cfgPath), "ship = \"local\"", "ship = \"mr\"", 1)
	testenv.WriteFile(t, cfgPath, cfg+"\n[shipping]\narchive = false\n[fleet]\nmax_runs = 2\n")
	for _, pair := range [][2]string{{buildChange, "alpha"}, {ciChange, "beta"}} {
		testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", pair[0], "tuckerbox.toml"), fmt.Sprintf("machines = [%q]\n", pair[1]))
	}
	f.commit("fleet shipping and placement")
	f.approveAll(buildChange, ciChange)
	f.push()
	binary := fleetCrashCLI(t)
	remote := filepath.Join(filepath.Dir(f.repo), "origin.git")
	api := fleetforge.Client{Dir: t.TempDir()}
	if err := api.Init(ctx, remote); err != nil {
		t.Fatal(err)
	}
	alpha := f.machine("alpha", 1, "")
	beta := f.machine("beta", 1, "")
	for _, m := range []*otherMachine{alpha, beta} {
		m.env = append(m.env, "FLEET_E2E_FORGE="+api.Dir, config.TestForgeURLEnv+"=https://github.com/example/fleet.git", "FAKEAGENT_PLAN="+f.env.Plan, "FAKEAGENT_RECORD="+f.env.Record)
		for k, v := range testenv.NoNetwork {
			m.env = append(m.env, k+"="+v)
		}
		testenv.WriteFile(t, filepath.Join(f.ssh, m.name, "tuckerbox"), binary)
		testenv.WriteFile(t, filepath.Join(f.ssh, m.name, "env"), strings.Join(m.env, "\n")+"\n")
	}
	// Always end detached engines before fixture directories/binaries disappear,
	// including an assertion failure while an engine is waiting for CI.
	t.Cleanup(func() {
		f.open("build")
		_ = api.SetCI(ctx, "green")
		for _, m := range []*otherMachine{alpha, beta} {
			for _, row := range m.status().Runs {
				st, err := statedir.Resolve(ctx, row.Worktree)
				if err != nil || !runs.Active(st.RunLock()) {
					continue
				}
				r, err := runs.ReadRun(st.RunFile())
				if err == nil {
					fleetCrashKill(t, r)
				}
			}
		}
	})
	f.env.SetPlan(t, map[string]testenv.Action{buildChange + ":build": {WaitFor: f.gate("build"), Touch: "build-work.txt", Tick: []string{"1.1"}}, ciChange + ":build": {Touch: "ci-work.txt", Tick: []string{"1.1"}}})
	placed := f.dispatchJSON()
	if len(placed.Started) != 2 || len(placed.Failed) != 0 {
		t.Fatalf("dispatch: %+v", placed)
	}
	for _, m := range []*otherMachine{alpha, beta} {
		if got := f.startCalls(m.name); len(got) != 1 || !strings.Contains(got[0], "--dispatched") {
			t.Fatalf("%s SSH starts: %v", m.name, got)
		}
	}
	waitFor(t, "alpha running build", func() bool {
		r := fleetCrashRun(t, alpha, buildChange)
		return r.State == runs.Running && r.Step == "build" && r.AgentPid > 0
	})
	waitFor(t, "beta waiting for CI", func() bool {
		s, err := api.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range s.Entries {
			if e.Branch == ciChange && e.Checks > 0 {
				return true
			}
		}
		return false
	})
	oldBuild := fleetCrashRun(t, alpha, buildChange)
	oldCI := fleetCrashRun(t, beta, ciChange)
	testenv.WriteFile(t, filepath.Join(oldBuild.Worktree, "unfinished-build.txt"), "unpublished old-machine work\n")
	if oldCI.State != runs.Shipping || oldCI.LastPush == "" || oldCI.MR == nil {
		t.Fatalf("not in CI wait: %+v", oldCI)
	}
	oldBuildFile := fleetCrashSnapshot(t, oldBuild)
	oldCIFile := fleetCrashSnapshot(t, oldCI)
	oldCITip := testenv.Git(t, remote, "rev-parse", "refs/heads/"+ciChange)
	fleetCrashKill(t, oldBuild)
	fleetCrashKill(t, oldCI)
	original := f.claims()
	for _, r := range []*runs.Run{oldBuild, oldCI} {
		if c := original[r.Change]; c.State != fleet.Active || c.Machine != r.FleetClaim.Machine {
			t.Fatalf("crash parked or lost claim: %+v", c)
		}
	}
	// Renewal with an injected test clock preserves holder/run/epoch and last
	// pushed head while making both otherwise-active leases expired.
	past := time.Now().Add(-time.Hour)
	clock := fleet.Store{Dir: f.repo, Now: func() time.Time { return past }}
	for _, r := range []*runs.Run{oldBuild, oldCI} {
		if err := clock.Renew(ctx, *r.FleetClaim); err != nil {
			t.Fatal(err)
		}
	}
	for id, c := range f.claims() {
		if !c.Expired(time.Now(), 10*time.Minute) {
			t.Fatalf("%s not expired: %+v", id, c)
		}
	}
	f.open("build")
	if err := api.SetCI(ctx, "green"); err != nil {
		t.Fatal(err)
	}
	fleetCrashMust(t, beta, binary, "run", "--take-over", buildChange)
	fleetCrashMust(t, alpha, binary, "run", "--take-over", ciChange)
	beta.waitState(buildChange, "ready")
	alpha.waitState(ciChange, "ready")
	current := f.claims()
	for _, p := range [][2]string{{buildChange, "beta"}, {ciChange, "alpha"}} {
		c := current[p[0]]
		if c.Machine != p[1] || c.Epoch != original[p[0]].Epoch+1 || c.State != fleet.Ready {
			t.Fatalf("takeover claim: %+v", c)
		}
	}
	if text := readFile(t, filepath.Join(oldBuild.Worktree, "unfinished-build.txt")); text != "unpublished old-machine work\n" {
		t.Fatal("takeover changed unpublished old-machine work")
	}
	recoveredBuild := fleetCrashRun(t, beta, buildChange)
	if _, err := os.Stat(filepath.Join(recoveredBuild.Worktree, "unfinished-build.txt")); !os.IsNotExist(err) {
		t.Fatal("step takeover copied unpublished old-machine work")
	}
	recoveredCI := fleetCrashRun(t, alpha, ciChange)
	if len(recoveredCI.History) != 0 || recoveredCI.LastPush == oldCITip {
		t.Fatalf("CI takeover reran steps or kept stale pin: %+v", recoveredCI)
	}
	if parent := testenv.Git(t, remote, "rev-parse", recoveredCI.LastPush+"^"); parent != oldCITip {
		t.Fatalf("takeover did not append to old shipping head: %s", parent)
	}
	state, err := api.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Opens != 2 || state.Edits != 1 {
		t.Fatalf("duplicate MR or marker not replaced: %+v", state)
	}
	mr, err := api.GetMR(ctx, recoveredCI.MR.Number)
	if err != nil {
		t.Fatal(err)
	}
	marker, ok := ship.ParseMarker(mr.Body)
	if !ok || marker.Machine != "alpha" || marker.Epoch != current[ciChange].Epoch {
		t.Fatalf("stale MR marker: %s", mr.Body)
	}
	// The old worktrees and local results survive. A stale CI fix contains the
	// old published head, so branch ancestry alone would let it push.
	if text := readFile(t, filepath.Join(oldCI.Worktree, "ci-work.txt")); text == "" {
		t.Fatal("old worktree was changed")
	}
	testenv.WriteFile(t, filepath.Join(oldCI.Worktree, "stale-fix.txt"), "old holder wakes to fix CI\n")
	testenv.Git(t, oldCI.Worktree, "add", ".")
	testenv.Git(t, oldCI.Worktree, "commit", "-m", "stale CI fix")
	before := f.fleetTip()
	buildTip := testenv.Git(t, remote, "rev-parse", "refs/heads/"+buildChange)
	ciTip := testenv.Git(t, remote, "rev-parse", "refs/heads/"+ciChange)
	for _, old := range []struct {
		m            *otherMachine
		file, change string
	}{{alpha, oldBuildFile, buildChange}, {beta, oldCIFile, ciChange}} {
		for _, effect := range []string{"push", "renew"} {
			r := fleetCrashTB(t, old.m, binary, "fleet-test-effect", effect, old.file)
			if r.code == 0 || !strings.Contains(r.stderr, "claim taken by") {
				t.Fatalf("stale %s accepted: %+v", effect, r)
			}
		}
		r := fleetCrashTB(t, old.m, binary, "resume", "--foreground", old.change)
		if r.code == 0 || !strings.Contains(r.stderr, "claim taken by") {
			t.Fatalf("old run resumed: %+v", r)
		}
	}
	staleMerge := fleetCrashTB(t, beta, binary, "fleet-test-effect", "merge", oldCIFile)
	if staleMerge.code == 0 || !strings.Contains(staleMerge.stderr, "pinned head changed") {
		t.Fatalf("stale pin merged: %+v", staleMerge)
	}
	if f.fleetTip() != before || testenv.Git(t, remote, "rev-parse", "refs/heads/"+buildChange) != buildTip || testenv.Git(t, remote, "rev-parse", "refs/heads/"+ciChange) != ciTip {
		t.Fatal("stale holder changed a remote ref")
	}
	state, err = api.Snapshot(ctx)
	if err != nil || state.AcceptedMerges != 0 || state.RefusedMerges != 1 {
		t.Fatalf("stale merge accounting: %+v %v", state, err)
	}
}
