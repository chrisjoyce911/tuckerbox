package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/dispatch"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

var (
	fakeSSHOnce sync.Once
	fakeSSHDir  string
	fakeSSHErr  error
)

// useFakeSSH puts the fake ssh (testdata/fakessh) first on PATH for the
// rest of the test, and returns the folder it reads and records in.
func useFakeSSH(t *testing.T) string {
	t.Helper()
	fakeSSHOnce.Do(func() {
		fakeSSHDir, fakeSSHErr = os.MkdirTemp("", "tuckerbox-fakessh-")
		if fakeSSHErr != nil {
			return
		}
		name := "ssh"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", filepath.Join(fakeSSHDir, name), "./testdata/fakessh")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			fakeSSHErr = fmt.Errorf("building fake ssh: %v: %s", err, out)
		}
	})
	if fakeSSHErr != nil {
		t.Fatal(fakeSSHErr)
	}
	dir := t.TempDir()
	t.Setenv("PATH", fakeSSHDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKESSH_DIR", dir)
	return dir
}

// fleetFixture is a fleet repository with a bare remote, this machine
// (studio, no fleet capacity) and the machines reached over the fake ssh.
type fleetFixture struct {
	*cliFixture
	ssh string
}

// push publishes main to the bare remote, which dispatch reads its base
// from.
func (f *cliFixture) push() {
	f.t.Helper()
	testenv.Git(f.t, f.repo, "push", "-q", "origin", "main")
}

func newFleet(t *testing.T, changes []string, repoTop, machineExtra string, ssh ...string) *fleetFixture {
	t.Helper()
	f := &fleetFixture{cliFixture: newCLI(t, testenv.RepoOptions{Changes: changes, RequireApproval: true})}
	f.ssh = useFakeSSH(t)
	f.setTop("mode = \"fleet\"\n" + repoTop)
	f.withRemote()
	var m strings.Builder
	m.WriteString("machine = \"studio\"\n" + machineExtra + "\n")
	for _, name := range ssh {
		fmt.Fprintf(&m, "[machines.%s]\nssh = %q\n", name, name)
	}
	machineConfig(t, f.env.Home, m.String())
	return f
}

// setTop puts lines at the top of the repository config and commits.
func (f *cliFixture) setTop(lines string) {
	f.t.Helper()
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data, err := os.ReadFile(p)
	if err != nil {
		f.t.Fatal(err)
	}
	testenv.WriteFile(f.t, p, lines+string(data))
	f.commit("config")
}

// report sets what machine name answers to tuckerbox machine --json.
func (f *fleetFixture) report(name string, r dispatch.Report) {
	f.t.Helper()
	r.Name = name
	if r.Agents == nil {
		r.Agents = []dispatch.Agent{{Name: "fake", Installed: true, SignIn: dispatch.Unknown}}
	}
	data, _ := json.Marshal(r)
	testenv.WriteFile(f.t, filepath.Join(f.ssh, name, "report.json"), string(data))
}

// answer sets what a machine's other commands print and exit with.
func (f *fleetFixture) answer(name, out string, code int) {
	f.t.Helper()
	testenv.WriteFile(f.t, filepath.Join(f.ssh, name, "out"), out)
	testenv.WriteFile(f.t, filepath.Join(f.ssh, name, "exit"), fmt.Sprint(code))
}

// calls lists the remote command lines the fake ssh was asked to run on
// dest, in order.
func (f *fleetFixture) calls(dest string) []string {
	f.t.Helper()
	file, err := os.Open(filepath.Join(f.ssh, "calls.log"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var out []string
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		var args []string
		if json.Unmarshal(sc.Bytes(), &args) != nil {
			continue
		}
		for i, a := range args {
			if a == "--" && i > 0 && args[i-1] == dest {
				out = append(out, strings.Join(args[i+1:], " "))
			}
		}
	}
	return out
}

func (f *fleetFixture) startCalls(dest string) []string {
	var out []string
	for _, c := range f.calls(dest) {
		if strings.HasPrefix(c, "tuckerbox run ") {
			out = append(out, c)
		}
	}
	return out
}

// claim writes change's claim straight into the bare remote's fleet ref.
func (f *fleetFixture) claim(change, machine, state string, renewed time.Time) {
	f.t.Helper()
	bare := filepath.Join(filepath.Dir(f.repo), "origin.git")
	parent := strings.TrimSpace(gitQuiet(bare, "rev-parse", "--verify", "--quiet", "refs/tuckerbox/fleet"))
	idx := filepath.Join(f.t.TempDir(), "index")
	env := append(os.Environ(), "GIT_INDEX_FILE="+idx)
	run := func(input string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = bare
		cmd.Env = env
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.Output()
		if err != nil {
			f.t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	if parent != "" {
		run("", "read-tree", parent)
	} else {
		run("", "read-tree", "--empty")
	}
	body := fmt.Sprintf("machine = %q\nrun = \"r1\"\nepoch = 1\nstate = %q\nrenewed_at = %s\n", machine, state, renewed.UTC().Format(time.RFC3339))
	blob := run(body, "hash-object", "-w", "--stdin")
	run("", "update-index", "--add", "--cacheinfo", "100644,"+blob+",claims/"+change+".toml")
	tree := run("", "write-tree")
	args := []string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit-tree", tree, "-m", "claim " + change}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	commit := run("", args...)
	run("", "update-ref", "refs/tuckerbox/fleet", commit)
}

func gitQuiet(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, _ := cmd.Output()
	return string(out)
}

func (f *fleetFixture) fleetTip() string {
	return strings.TrimSpace(gitQuiet(filepath.Join(filepath.Dir(f.repo), "origin.git"), "rev-parse", "--verify", "--quiet", "refs/tuckerbox/fleet"))
}

func (f *fleetFixture) dispatchJSON(args ...string) dispatchResult {
	f.t.Helper()
	res := f.tb(append([]string{"dispatch", "--json"}, args...)...)
	if res.code != 0 {
		f.t.Fatalf("dispatch: %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	var out dispatchResult
	if err := json.Unmarshal([]byte(res.stdout), &out); err != nil {
		f.t.Fatalf("dispatch --json: %v\n%s", err, res.stdout)
	}
	return out
}

func skipped(r dispatchResult, id string) string {
	for _, s := range r.Skipped {
		if s.Change == id {
			return s.Why + " " + strings.Join(s.Notes, "; ")
		}
	}
	return ""
}

var mini = dispatch.Report{FleetCapacity: 2, Checkout: true}

// operating-modes: Dispatch in a solo repository.
func TestDispatchInSoloRepository(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	f.approveAll("add-login")
	res := f.tb("dispatch")
	if res.code == 0 || !strings.Contains(res.stderr, "solo") {
		t.Errorf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	f.noRun("add-login")
}

// team-claims: Dispatch in a team repository.
func TestDispatchInTeamRepository(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data, _ := os.ReadFile(p)
	text := strings.Replace(string(data), "ship = \"local\"\n", "ship = \"mr\"\n", 1)
	testenv.WriteFile(t, p, "mode = \"team\"\n"+text+"\n[team]\napprovers = [\"sam\"]\n")
	f.commit("team")
	// The forge comes from the remote; dispatch refuses before any fetch.
	testenv.Git(t, f.repo, "remote", "add", "origin", "https://github.com/sam/app.git")
	res := f.tb("dispatch")
	if res.code == 0 || !strings.Contains(res.stderr, "each person starts their own runs") {
		t.Errorf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	f.noRun("add-login")
}

// Scenario: Nothing to do.
func TestDispatchNothingToDo(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "mini")
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-users", "tuckerbox.toml"), "depends_on = [\"add-login\"]\n")
	f.commit("deps")
	f.push()
	f.approveAll("add-login", "add-users")
	f.report("mini", mini)
	f.claim("add-login", "mini", dispatch.ClaimActive, time.Now())
	res := f.tb("dispatch")
	if res.code != 0 {
		t.Fatalf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{"add-login: claimed by mini (active)", "add-users: waiting on add-login"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, res.stdout)
		}
	}
	if c := f.startCalls("mini"); len(c) != 0 {
		t.Errorf("started %v", c)
	}
}

// Scenario: Unapproved change skipped.
func TestDispatchUnapprovedChangeSkipped(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "mini")
	f.report("mini", mini)
	r := f.dispatchJSON()
	if len(r.Started) != 0 || !strings.HasPrefix(skipped(r, "add-login"), "not approved") {
		t.Errorf("%+v", r)
	}
	if c := f.startCalls("mini"); len(c) != 0 {
		t.Errorf("started %v", c)
	}
}

// Scenario: Dry run: the placement with each candidate's reasons, nothing
// started, the fleet ref unchanged.
func TestDispatchDryRun(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "lab", "mini")
	f.approveAll("add-login", "add-users")
	f.report("mini", dispatch.Report{FleetCapacity: 1, Checkout: true})
	f.report("lab", dispatch.Report{FleetCapacity: 1, Checkout: false})
	f.claim("other", "mini", dispatch.ClaimReady, time.Now())
	before := f.fleetTip()
	res := f.tb("dispatch", "--dry-run")
	if res.code != 0 {
		t.Fatalf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{"would start add-login on mini", "add-users: no machine fits", "  lab: no checkout of this repository", "  mini: no free fleet capacity"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, res.stdout)
		}
	}
	if c := append(f.startCalls("mini"), f.startCalls("lab")...); len(c) != 0 {
		t.Errorf("started %v", c)
	}
	if after := f.fleetTip(); after != before {
		t.Errorf("fleet ref moved: %s -> %s", before, after)
	}
	r := f.dispatchJSON("--dry-run")
	if !r.DryRun || len(r.Actions) != 1 || r.Actions[0].Machine != "mini" || len(r.Started) != 0 {
		t.Errorf("json %+v", r)
	}
}

// Scenario: Start over SSH; Agent signed out; Sign-in unknown.
func TestDispatchStartOverSSH(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "lab", "mini")
	f.approveAll("add-login")
	f.report("mini", dispatch.Report{FleetCapacity: 1, Checkout: true})
	f.report("lab", dispatch.Report{FleetCapacity: 3, Checkout: true,
		Agents: []dispatch.Agent{{Name: "fake", Installed: true, SignIn: dispatch.SignedOut}}})
	f.answer("mini", "add-login: started (pid 7, queued) in /w\n", 0)
	r := f.dispatchJSON()
	if len(r.Started) != 1 || r.Started[0].Machine != "mini" {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(strings.Join(r.Started[0].Notes, ";"), "lab: fake signed out") ||
		!strings.Contains(strings.Join(r.Started[0].Notes, ";"), "mini: fake sign-in unknown") {
		t.Errorf("notes %v", r.Started[0].Notes)
	}
	want := "tuckerbox run --for " + shellWord(r.Remote) + " --dispatched add-login"
	if c := f.startCalls("mini"); len(c) != 1 || c[0] != want {
		t.Errorf("mini ran %q, want %q", c, want)
	}
	if c := f.startCalls("lab"); len(c) != 0 {
		t.Errorf("lab ran %v", c)
	}
}

// shellWord quotes s as the remote runner does, for comparing calls.
func shellWord(s string) string {
	if strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@,+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Placement is advice: a refused claim is reported, not retried on another
// machine in the same pass (the half of "Two dispatchers" this side sees).
func TestDispatchRefusedStartNotRetried(t *testing.T) {
	f := newFleet(t, []string{"add-login"}, "", "", "lab", "mini")
	f.approveAll("add-login")
	f.report("mini", dispatch.Report{FleetCapacity: 2, Checkout: true})
	f.report("lab", dispatch.Report{FleetCapacity: 1, Checkout: true})
	testenv.WriteFile(t, filepath.Join(f.ssh, "mini", "out"), "")
	testenv.WriteFile(t, filepath.Join(f.ssh, "mini", "exit"), "1")
	// The fake prints nothing on stderr for "out"; the runner reports the
	// exit, which is enough to see a failed start.
	r := f.dispatchJSON()
	if len(r.Failed) != 1 || r.Failed[0].Machine != "mini" || len(r.Started) != 0 {
		t.Fatalf("%+v", r)
	}
	if c := f.startCalls("lab"); len(c) != 0 {
		t.Errorf("retried on lab: %v", c)
	}
}

// Scenario: Holder unreachable.
func TestDispatchHolderUnreachable(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "mini", "lab")
	f.approveAll("add-login", "add-users")
	testenv.WriteFile(t, filepath.Join(f.ssh, "lab", "asleep"), "")
	f.report("mini", mini)
	f.answer("mini", "started\n", 0)
	f.claim("add-login", "lab", dispatch.ClaimReady, time.Now())
	res := f.tb("dispatch")
	if res.code != 0 {
		t.Fatalf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	for _, want := range []string{"lab: unreachable", "add-login: ready on lab, which is unreachable", "start add-users on mini"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("output lacks %q:\n%s", want, res.stdout)
		}
	}
	c := f.startCalls("mini")
	if len(c) != 1 || !strings.HasSuffix(c[0], "--dispatched add-users") {
		t.Errorf("mini ran %v", c)
	}
}

// Scenario: Continuations first, through the CLI: the holder is sent the
// conditional next-group command with what its report said.
func TestDispatchContinuationsFirst(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users", "add-roles"}, "", "", "mini")
	cfgPath := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	testenv.WriteFile(t, cfgPath, readFile(t, cfgPath)+"\n[fleet]\nmax_runs = 2\n")
	f.commit("limit")
	f.push()
	f.approveAll("add-login", "add-users", "add-roles")
	since := time.Date(2026, 11, 13, 9, 0, 0, 123, time.UTC)
	f.report("mini", dispatch.Report{FleetCapacity: 2, Checkout: true,
		Runs: []dispatch.RunReport{{Change: "add-login", State: "ready", Since: since, Scope: "1"}}})
	f.answer("mini", "started\n", 0)
	f.claim("add-login", "mini", dispatch.ClaimReady, time.Now())
	f.claim("add-roles", "mini", dispatch.ClaimActive, time.Now())
	r := f.dispatchJSON()
	if len(r.Started) != 1 || r.Started[0].Kind != dispatch.Continue || r.Started[0].Change != "add-login" {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(skipped(r, "add-users"), "fleet limit reached") {
		t.Errorf("add-users: %q", skipped(r, "add-users"))
	}
	want := "tuckerbox run --for " + shellWord(r.Remote) + " --expect-state ready --expect-since " + since.Format(time.RFC3339Nano) + " --tasks next add-login"
	if c := f.startCalls("mini"); len(c) != 1 || c[0] != want {
		t.Errorf("mini ran %q, want %q", c, want)
	}
}

// run --for finds this machine's checkout of a remote; a remote with no
// checkout here is refused.
func TestRunForRemote(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "tasks.md"), "# Tasks\n\n## 1. One\n\n- [ ] 1.1 A\n\n## 2. Two\n\n- [ ] 2.1 B\n")
	f.commit("groups")
	f.withRemote()
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1"}}})
	elsewhere := t.TempDir()
	key := filepath.ToSlash(filepath.Join(filepath.Dir(f.repo), "origin.git"))
	res := tbIn(t, elsewhere, "run", "--for", "example.invalid/none", "add-login")
	if res.code == 0 || !strings.Contains(res.stderr, "no checkout of example.invalid/none") {
		t.Errorf("unknown remote: %d %s", res.code, res.stderr)
	}
	// The repository is known to the machine index once it has had a run.
	if res := f.tb("run", "--foreground", "--tasks", "1", "add-login"); res.code != 0 {
		t.Fatalf("first run: %d %s", res.code, res.stderr)
	}
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"2.1"}}})
	res = tbIn(t, elsewhere, "run", "--for", key, "--tasks", "next", "add-login")
	if res.code != 0 {
		t.Fatalf("run --for: %d %s %s", res.code, res.stdout, res.stderr)
	}
	f.waitState("add-login", "ready", "")
}

// Scenario: A person reran and stopped it meanwhile (the holder's half):
// a continuation whose since no longer matches starts nothing.
func TestRunExpectStateMismatch(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "tasks.md"), "# Tasks\n\n## 1. One\n\n- [ ] 1.1 A\n\n## 2. Two\n\n- [ ] 2.1 B\n")
	f.commit("groups")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1"}}})
	if res := f.tb("run", "--foreground", "--tasks", "1", "add-login"); res.code != 0 {
		t.Fatalf("scope 1: %d %s %s", res.code, res.stdout, res.stderr)
	}
	before := f.runOf("add-login")
	stale := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	res := f.tb("run", "--expect-state", "ready", "--expect-since", stale, "--tasks", "next", "add-login")
	if res.code == 0 || !strings.Contains(res.stdout+res.stderr, "changed since it was looked at") {
		t.Errorf("stale since: %d %s %s", res.code, res.stdout, res.stderr)
	}
	after := f.runOf("add-login")
	if after.State != before.State || !after.Since.Equal(before.Since) {
		t.Errorf("run changed: %+v -> %+v", before, after)
	}
	res = f.tb("run", "--expect-state", "stopped", "--expect-since", before.Since.Format(time.RFC3339Nano), "--tasks", "next", "add-login")
	if res.code == 0 {
		t.Errorf("wrong state accepted: %s", res.stdout)
	}
	if res := f.tb("run", "--expect-state", "ready", "add-login"); res.code != 2 {
		t.Errorf("--expect-state without --expect-since: %d", res.code)
	}
}

// operating-modes: Changing mode. After a move from fleet to solo,
// dispatch refuses, starts nothing on any machine, and leaves the fleet
// claims as they were.
func TestDispatchAfterFleetToSolo(t *testing.T) {
	f := newFleet(t, []string{"add-login", "add-users"}, "", "", "mini")
	f.approveAll("add-login", "add-users")
	f.report("mini", mini)
	f.answer("mini", "started\n", 0)
	f.claim("add-login", "mini", dispatch.ClaimActive, time.Now())
	before := f.fleetTip()
	p := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	testenv.WriteFile(t, p, strings.Replace(readFile(t, p), "mode = \"fleet\"\n", "", 1))
	f.commit("back to solo")
	f.push()
	res := f.tb("dispatch")
	if res.code == 0 || !strings.Contains(res.stderr, "solo") {
		t.Errorf("%d %s %s", res.code, res.stdout, res.stderr)
	}
	if c := f.startCalls("mini"); len(c) != 0 {
		t.Errorf("started %v", c)
	}
	if after := f.fleetTip(); after != before {
		t.Errorf("fleet ref moved: %s -> %s", before, after)
	}
}

// Scenario: Unapproved before the command arrives (the holder's half): a
// continuation that matches the run but finds the change unapproved
// starts nothing.
func TestRunExpectStateUnapproved(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "add-login", "tasks.md"), "# Tasks\n\n## 1. One\n\n- [ ] 1.1 A\n\n## 2. Two\n\n- [ ] 2.1 B\n")
	f.commit("groups")
	f.approveAll("add-login")
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"1.1"}}})
	if res := f.tb("run", "--foreground", "--tasks", "1", "add-login"); res.code != 0 {
		t.Fatalf("scope 1: %d %s %s", res.code, res.stdout, res.stderr)
	}
	before := f.runOf("add-login")
	if res := f.tb("unapprove", "add-login"); res.code != 0 {
		t.Fatalf("unapprove: %d %s", res.code, res.stderr)
	}
	res := f.tb("run", "--expect-state", "ready", "--expect-since", before.Since.Format(time.RFC3339Nano), "--tasks", "next", "add-login")
	if res.code == 0 {
		t.Errorf("started while unapproved: %s", res.stdout)
	}
	after := f.runOf("add-login")
	if after.State != before.State || !after.Since.Equal(before.Since) {
		t.Errorf("run changed: %+v -> %+v", before, after)
	}
}
