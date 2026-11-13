package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// otherMachine is another machine of the fleet: its own clone of the bare
// remote and its own state and config directories, reached by the fake
// ssh, which runs the real tuckerbox there.
type otherMachine struct {
	f    *fleetFixture
	name string
	repo string
	env  []string
}

// machine adds machine name, with fleet_capacity capacity and machine
// config extra, reached from this machine as [machines.<name>].
func (f *fleetFixture) machine(name string, capacity int, extra string) *otherMachine {
	f.t.Helper()
	t := f.t
	bare := filepath.Join(filepath.Dir(f.repo), "origin.git")
	root := filepath.Join(t.TempDir(), name)
	repo := filepath.Join(root, "repo")
	testenv.Git(t, filepath.Dir(f.repo), "clone", "-q", bare, repo)
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", name + "@example.invalid"}, {"commit.gpgsign", "false"}} {
		testenv.Git(t, repo, "config", kv[0], kv[1])
	}
	home := filepath.Join(root, "home")
	env := []string{"XDG_STATE_HOME=" + filepath.Join(home, "state"), "XDG_CONFIG_HOME=" + filepath.Join(home, "config")}
	// The remote runner passes only a clean environment, as real ssh
	// would; the test harness's own settings (the fake agent's plan, the
	// fake hc, no network) reach the machine through its env file.
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := testenv.NoNetwork[k]; ok || strings.HasPrefix(k, "FAKEAGENT_") || strings.HasPrefix(k, "FAKEHC_") {
			env = append(env, kv)
		}
	}
	testenv.WriteFile(t, filepath.Join(home, "config", "tuckerbox", "config.toml"),
		fmt.Sprintf("machine = %q\nfleet_capacity = %d\n%s", name, capacity, extra))
	d := filepath.Join(f.ssh, name)
	testenv.WriteFile(t, filepath.Join(d, "tuckerbox"), tools.Tuckerbox)
	testenv.WriteFile(t, filepath.Join(d, "cwd"), repo)
	testenv.WriteFile(t, filepath.Join(d, "env"), strings.Join(env, "\n")+"\n")
	// A machine's checkout is found through its machine index, which only
	// knows repositories that had a run there: list the clone.
	seed := filepath.Join(home, "state", "tuckerbox", "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	ix, _ := json.Marshal(map[string]any{"version": 1, "runs": []map[string]string{{"repo": repo, "change": "seed", "worktree": repo, "state": seed}}})
	testenv.WriteFile(t, filepath.Join(home, "state", "tuckerbox", "index.json"), string(ix))
	m := &otherMachine{f: f, name: name, repo: repo, env: env}
	t.Cleanup(m.drain)
	return m
}

func (m *otherMachine) cmd(args ...string) *exec.Cmd {
	cmd := exec.Command(tools.Tuckerbox, args...)
	cmd.Dir = m.repo
	cmd.Env = append(os.Environ(), m.env...)
	return cmd
}

func (m *otherMachine) tb(args ...string) cmdResult {
	m.f.t.Helper()
	cmd := m.cmd(args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		m.f.t.Fatalf("%s: tuckerbox %v: %v", m.name, args, err)
	}
	return cmdResult{out.String(), errOut.String(), code}
}

func (m *otherMachine) must(args ...string) cmdResult {
	m.f.t.Helper()
	res := m.tb(args...)
	if res.code != 0 {
		m.f.t.Fatalf("%s: tuckerbox %v: %d\n%s\n%s", m.name, args, res.code, res.stdout, res.stderr)
	}
	return res
}

func (m *otherMachine) status() statusDoc {
	m.f.t.Helper()
	var d statusDoc
	res := m.tb("status", "--json")
	if err := json.Unmarshal([]byte(res.stdout), &d); err != nil {
		m.f.t.Fatalf("%s status: %v\n%s", m.name, err, res.stdout)
	}
	return d
}

func (m *otherMachine) state(change string) string {
	for _, r := range m.status().Runs {
		if r.Change == change {
			return r.State
		}
	}
	return ""
}

func (m *otherMachine) waitState(change, state string) {
	m.f.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		s := m.state(change)
		if s == state {
			return
		}
		if time.Now().After(deadline) {
			m.f.t.Fatalf("%s: timed out waiting for %s to be %s (is %q)", m.name, change, state, s)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// drain opens every gate and waits for this machine's runs to end.
func (m *otherMachine) drain() {
	for _, g := range []string{"a", "b", "c", "build", "review", "never"} {
		m.f.open(g)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, r := range m.status().Runs {
			if r.State == "running" || r.State == "queued" {
				busy = true
			}
		}
		if !busy {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.f.t.Errorf("%s: runs still active at the end of the test", m.name)
}

// claims reads the fleet ref on the bare remote.
func (f *fleetFixture) claims() map[string]fleet.Claim {
	f.t.Helper()
	cs, err := fleet.Store{Dir: f.repo, Remote: "origin"}.Read(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	return cs
}

// hold makes the fake ssh wait before running verb on machine name, and
// returns a function that waits until it is held and one that releases it.
func (f *fleetFixture) hold(name, verb string) (held func(), release func()) {
	d := filepath.Join(f.ssh, name)
	testenv.WriteFile(f.t, filepath.Join(d, "hold-"+verb), "")
	held = func() {
		f.t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(d, "held-"+verb)); err == nil {
				return
			}
			if time.Now().After(deadline) {
				f.t.Fatalf("%s %s never held", name, verb)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	release = func() { testenv.WriteFile(f.t, filepath.Join(d, "release-"+verb), "") }
	return held, release
}

// background starts tuckerbox args in this machine's checkout and returns
// a function that waits for it and returns what it printed.
func (f *fleetFixture) background(args ...string) func() cmdResult {
	f.t.Helper()
	cmd := exec.Command(tools.Tuckerbox, args...)
	cmd.Dir = f.repo
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	return func() cmdResult {
		err := cmd.Wait()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return cmdResult{out.String(), errOut.String(), code}
	}
}

const twoGroups = "# Tasks\n\n## 1. One\n\n- [ ] 1.1 A\n\n## 2. Two\n\n- [ ] 2.1 B\n"

// tasksAndPublish writes change id's tasks.md, commits and pushes it.
func (f *fleetFixture) tasksAndPublish(id, text string) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.repo, "openspec", "changes", id, "tasks.md"), text)
	f.commit("tasks of " + id)
	f.push()
}

// Scenario: Start over SSH, end to end: the run on mini takes a fleet slot
// and claims the change.
func TestDispatchStartOverSSHClaims(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "mini")
	f.approveAll("add-login")
	f.push()
	mini := f.machine("mini", 1, "")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1"}}})
	r := f.dispatchJSON()
	if len(r.Started) != 1 || r.Started[0].Machine != "mini" {
		t.Fatalf("%+v", r)
	}
	want := "tuckerbox run --for " + shellWord(r.Remote) + " --dispatched add-login"
	if c := f.startCalls("mini"); len(c) != 1 || c[0] != want {
		t.Errorf("mini ran %q, want %q", c, want)
	}
	if c := f.claims()["add-login"]; c.Machine != "mini" {
		t.Errorf("claim %+v", c)
	}
	mini.waitState("add-login", "ready")
	// The run was dispatched, and its slot is free again once ready.
	var rep struct {
		FleetRuns int `json:"fleet_runs"`
		Runs      []struct {
			Change     string `json:"change"`
			Dispatched bool   `json:"dispatched"`
		} `json:"runs"`
	}
	res := mini.must("machine", "--json", "--for", r.Remote)
	if err := json.Unmarshal([]byte(res.stdout), &rep); err != nil || len(rep.Runs) != 1 || !rep.Runs[0].Dispatched || rep.FleetRuns != 0 {
		t.Errorf("mini's report: %v %s", err, res.stdout)
	}
	if c := f.claims()["add-login"]; c.State != fleet.Ready {
		t.Errorf("claim %+v", c)
	}
}

// Scenario: Two dispatchers. Dispatch on studio and on mini both place
// add-login: one run claims it, the other refuses with "claimed by", and
// both outputs say what happened.
func TestTwoDispatchers(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "lab")
	f.approveAll("add-login")
	f.push()
	f.env.SetPlan(t, map[string]testenv.Action{"build": {WaitFor: f.gate("build"), Tick: []string{"1.1"}}})
	mini := f.machine("mini", 1, "")
	lab := f.machine("lab", 1, "")
	_ = lab
	// studio's dispatcher reads the fleet ref (no claims), then waits for
	// lab's report; meanwhile mini's own dispatcher places add-login on
	// mini and its run claims it.
	held, release := f.hold("lab", "machine")
	studio := f.background("dispatch")
	held()
	res := mini.must("dispatch")
	if !strings.Contains(res.stdout, "start add-login on mini") {
		t.Fatalf("mini's dispatch:\n%s\n%s", res.stdout, res.stderr)
	}
	release()
	out := studio()
	if out.code != 0 || !strings.Contains(out.stdout, "start add-login on lab failed") || !strings.Contains(out.stdout, "claimed by mini") {
		t.Errorf("studio's dispatch: %d\n%s\n%s", out.code, out.stdout, out.stderr)
	}
	if c := f.claims()["add-login"]; c.Machine != "mini" {
		t.Errorf("claim %+v", c)
	}
	if s := lab.state("add-login"); s != "" {
		t.Errorf("lab has a run (%s)", s)
	}
}

// readyAfterGroupOne makes add-login ready after group 1 on studio, by a
// person's run there, so its claim is ready and group 2 unticked.
func readyAfterGroupOne(t *testing.T, f *fleetFixture, studio *otherMachine) {
	t.Helper()
	f.env.SetPlan(t, map[string]testenv.Action{"add-login:build": {Tick: []string{"1.1"}}})
	studio.must("run", "--tasks", "1", "add-login")
	studio.waitState("add-login", "ready")
	if c := f.claims()["add-login"]; c.Machine != "studio" || c.State != fleet.Ready {
		t.Fatalf("claim after group 1: %+v", c)
	}
}

// Scenario: Next group across passes.
func TestNextGroupAcrossPasses(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "mini", "studio")
	cfgPath := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	testenv.WriteFile(t, cfgPath, readFile(t, cfgPath)+"\n[fleet]\nmax_runs = 1\n")
	f.commit("limit")
	f.tasksAndPublish("add-login", twoGroups)
	f.approveAll("add-login", "add-users")
	f.push()
	studio := f.machine("studio", 0, "")
	mini := f.machine("mini", 0, "")
	readyAfterGroupOne(t, f, studio)
	// add-users takes the only room, on mini.
	f.env.SetPlan(t, map[string]testenv.Action{"add-login:build": {Tick: []string{"2.1"}},
		"add-users:build": {WaitFor: f.gate("b"), Tick: []string{"1.1"}}})
	mini.must("run", "add-users")
	mini.waitState("add-users", "running")
	r := f.dispatchJSON()
	if len(r.Started)+len(r.Failed) != 0 || !strings.Contains(skipped(r, "add-login"), "waiting for room") {
		t.Fatalf("first pass: %+v\nstudio: %s", r, studio.must("machine", "--json", "--for", r.Remote).stdout)
	}
	if c := f.startCalls("studio"); len(c) != 0 {
		t.Errorf("first pass ran on studio: %v", c)
	}
	f.open("b")
	mini.waitState("add-users", "ready")
	r = f.dispatchJSON()
	if len(r.Started) != 1 || r.Started[0].Change != "add-login" || r.Started[0].Machine != "studio" {
		t.Fatalf("second pass: %+v", r)
	}
	c := f.startCalls("studio")
	if len(c) != 1 || !strings.Contains(c[0], "--expect-state ready --expect-since ") || !strings.HasSuffix(c[0], "--tasks next add-login") {
		t.Errorf("studio ran %v", c)
	}
	studio.waitState("add-login", "ready")
	if c := f.claims()["add-login"]; c.Machine != "studio" || c.State != fleet.Ready {
		t.Errorf("claim after group 2: %+v", c)
	}
}

// Scenario: A person reran and stopped it meanwhile.
func TestPersonReranAndStoppedMeanwhile(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "studio")
	f.tasksAndPublish("add-login", twoGroups)
	f.approveAll("add-login")
	f.push()
	studio := f.machine("studio", 0, "")
	readyAfterGroupOne(t, f, studio)
	held, release := f.hold("studio", "run")
	pass := f.background("dispatch")
	held()
	// Between the report and the command: rerun scope 1, and stop it.
	f.env.SetPlan(t, map[string]testenv.Action{"add-login:build": {WaitFor: f.gate("never"), Tick: []string{"1.1"}}})
	studio.must("rerun", "add-login", "build")
	studio.waitState("add-login", "running")
	studio.must("stop", "add-login")
	studio.waitState("add-login", "stopped")
	release()
	out := pass()
	if out.code != 0 || !strings.Contains(out.stdout, "continue add-login --tasks next on studio failed") || !strings.Contains(out.stdout, "changed since it was looked at") {
		t.Errorf("dispatch: %d\n%s\n%s", out.code, out.stdout, out.stderr)
	}
	if s := studio.state("add-login"); s != "stopped" {
		t.Errorf("run is %s", s)
	}
	if c := f.claims()["add-login"]; c.State != fleet.Parked {
		t.Errorf("claim %+v", c)
	}
}

// Scenario: Unapproved before the command arrives.
func TestUnapprovedBeforeTheCommandArrives(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "studio")
	f.tasksAndPublish("add-login", twoGroups)
	f.approveAll("add-login")
	f.push()
	studio := f.machine("studio", 0, "")
	readyAfterGroupOne(t, f, studio)
	before := studio.status()
	held, release := f.hold("studio", "run")
	pass := f.background("dispatch")
	held()
	if res := f.tb("unapprove", "add-login"); res.code != 0 {
		t.Fatalf("unapprove: %d %s", res.code, res.stderr)
	}
	release()
	out := pass()
	if out.code != 0 || !strings.Contains(out.stdout, "continue add-login --tasks next on studio failed") {
		t.Errorf("dispatch: %d\n%s\n%s", out.code, out.stdout, out.stderr)
	}
	after := studio.status()
	if len(after.Runs) != 1 || after.Runs[0].State != "ready" || after.Runs[0].Since != before.Runs[0].Since {
		t.Errorf("run changed: %+v -> %+v", before.Runs, after.Runs)
	}
	if c := f.claims()["add-login"]; c.State != fleet.Ready {
		t.Errorf("claim %+v", c)
	}
}
