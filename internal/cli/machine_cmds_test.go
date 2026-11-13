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

	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/machines"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// fakeSSHEnv, when set, makes the test binary a fake ssh: the machine
// named by the destination is this computer with another state directory,
// FAKE_SSH_STATE_<dest>, where it runs FAKE_SSH_TUCKERBOX with the remote
// command's arguments. A destination without a state directory is a
// machine that can't be reached (ssh exits 255).
const fakeSSHEnv = "FAKE_SSH_TUCKERBOX"

func fakeSSH() int {
	args := os.Args[1:]
	// -T -o BatchMode=yes -o SendEnv=-* <dest> -- <command>
	if len(args) != 8 || args[0] != "-T" || args[2] != "BatchMode=yes" || args[4] != "SendEnv=-*" || args[6] != "--" {
		fmt.Fprintf(os.Stderr, "fake ssh: unexpected arguments %q\n", args)
		return 2
	}
	if rec := os.Getenv("FAKE_SSH_ENV_RECORD"); rec != "" {
		_ = os.WriteFile(rec, []byte(strings.Join(os.Environ(), "\n")), 0o644)
	}
	dest, remote := args[5], strings.Fields(args[7])
	state := os.Getenv("FAKE_SSH_STATE_" + dest)
	if state == "" {
		fmt.Fprintf(os.Stderr, "ssh: Could not resolve hostname %s\n", dest)
		return 255
	}
	if len(remote) == 0 || remote[0] != "tuckerbox" {
		return 127
	}
	cmd := exec.Command(os.Getenv(fakeSSHEnv), remote[1:]...)
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+state, fakeSSHEnv+"=")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// machineApp is an App with the fake ssh and agents found per installed,
// each answering its sign-in check with signIn[name].
func machineApp(t *testing.T, installed []string, signIn map[string]string) (App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	set := map[string]bool{}
	for _, n := range installed {
		set[n] = true
	}
	env := append(os.Environ(), fakeSSHEnv+"="+tools.Tuckerbox)
	return App{
		Stdout: &out, Stderr: &errOut,
		DoctorEnv: doctor.Env{
			LookPath: func(name string) (string, error) {
				if set[name] {
					return name, nil
				}
				return "", exec.ErrNotFound
			},
			Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
				if len(args) == 1 && args[0] == "--version" {
					return []byte("1.0.0\n"), nil
				}
				return []byte(signIn[path]), nil
			},
		},
		DoctorTools:   doctor.DefaultTools,
		MachineRunner: machines.Runner{SSH: exe, Environ: env, Keep: []string{"FAKE"}},
	}, &out, &errOut
}

func writeMachineConfig(t *testing.T, env *testenv.Env, data string) {
	t.Helper()
	testenv.WriteFile(t, filepath.Join(env.Home, "config", "tuckerbox", "config.toml"), data)
}

func machineReport(t *testing.T, a App, out *bytes.Buffer, args ...string) machines.Report {
	t.Helper()
	out.Reset()
	if code := a.Run(append([]string{"machine", "--json"}, args...)); code != 0 {
		t.Fatalf("machine --json: exit %d", code)
	}
	rep, err := machines.ParseReport(out.Bytes())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return rep
}

// Scenario: Machine not in the fleet. Without fleet_capacity a machine
// reports capacity 0.
func TestMachineNotInTheFleet(t *testing.T) {
	env := tools.Setup(t)
	writeMachineConfig(t, env, "machine = \"mini\"\n")
	a, out, _ := machineApp(t, nil, nil)
	rep := machineReport(t, a, out)
	if rep.Name != "mini" || rep.FleetCapacity != 0 || rep.FleetRuns != 0 || rep.Free() != 0 {
		t.Fatalf("report %+v", rep)
	}
	if rep.OS == "" || rep.Version == "" || rep.Format != machines.ReportVersion {
		t.Fatalf("report %+v", rep)
	}
}

// Scenario: Codex signed out.
func TestCodexSignedOut(t *testing.T) {
	env := tools.Setup(t)
	writeMachineConfig(t, env, "machine = \"mini\"\nfleet_capacity = 2\n\n[agents.omp]\ncommand = [\"omp\", \"{brief}\"]\n")
	a, out, _ := machineApp(t, []string{"codex", "claude", "omp"}, map[string]string{
		"codex": "Not logged in\n", "claude": `{"loggedIn": true, "email": "someone@example.com"}`,
	})
	rep := machineReport(t, a, out)
	codex, ok := rep.Agent("codex")
	if !ok || !codex.Installed || codex.SignedIn == nil || *codex.SignedIn || codex.SignIn != "signed out" {
		t.Fatalf("codex %+v", codex)
	}
	claude, _ := rep.Agent("claude")
	omp, _ := rep.Agent("omp")
	if claude.SignedIn == nil || !*claude.SignedIn || omp.SignedIn != nil || !omp.Installed {
		t.Fatalf("claude %+v omp %+v", claude, omp)
	}
	if rep.FleetCapacity != 2 {
		t.Fatalf("capacity %d", rep.FleetCapacity)
	}
	// Nothing from the agent's account reaches the report.
	if strings.Contains(out.String(), "example.com") {
		t.Fatalf("report holds account details:\n%s", out)
	}
}

// Scenario: No checkout.
func TestMachineNoCheckout(t *testing.T) {
	tools.Setup(t)
	a, out, _ := machineApp(t, nil, nil)
	rep := machineReport(t, a, out, "--for", "github.com/sam/app")
	if rep.Checkout || rep.Path != "" || len(rep.Runs) != 0 || rep.For != "github.com/sam/app" {
		t.Fatalf("report %+v", rep)
	}
	if !strings.Contains(out.String(), `"checkout": false`) {
		t.Fatalf("checkout false not printed:\n%s", out)
	}
}

// With a checkout, the report finds it through the machine index and
// lists its runs.
func TestMachineCheckoutAndRuns(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[agents.omp]\ncommand = [\"omp-cli\", \"{brief}\"]\n"})
	testenv.Git(t, f.repo, "remote", "add", "origin", "https://github.com/sam/app.git")
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run: %d %s", res.code, res.stderr)
	}
	a, out, _ := machineApp(t, []string{"omp-cli"}, nil)
	rep := machineReport(t, a, out, "--for", "git@github.com:sam/app.git")
	// The repository's own custom agent is reported, found by its program.
	if omp, ok := rep.Agent("omp"); !ok || !omp.Installed || omp.SignIn != "unknown" {
		t.Fatalf("repository agent omp: %+v %v", omp, ok)
	}
	if !rep.Checkout || !sameDir(rep.Path, f.repo) {
		t.Fatalf("checkout %v path %q, want %s", rep.Checkout, rep.Path, f.repo)
	}
	if len(rep.Runs) != 1 || rep.Runs[0].Change != "add-login" || rep.Runs[0].State != "ready" || rep.Runs[0].Dispatched || rep.Runs[0].Since == "" {
		t.Fatalf("runs %+v", rep.Runs)
	}
}

// Scenario: Runs on two machines. add-login runs on studio (this machine)
// and add-users on mini; status --all-machines on studio lists both,
// labelled, and names an unreachable machine without failing.
func TestRunsOnTwoMachines(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	if res := f.tb("run", "--foreground", "add-login"); res.code != 0 {
		t.Fatalf("run on studio: %d %s", res.code, res.stderr)
	}
	// mini: another repository and another state directory.
	miniState := filepath.Join(f.env.Home, "mini-state")
	miniRepo := tools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-users"}})
	cmd := exec.Command(tools.Tuckerbox, "run", "--foreground", "add-users")
	cmd.Dir = miniRepo
	cmd.Env = append(os.Environ(), "XDG_STATE_HOME="+miniState)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run on mini: %v\n%s", err, out)
	}
	t.Setenv("FAKE_SSH_STATE_mini", miniState)
	writeMachineConfig(t, f.env, "machine = \"studio\"\n\n[machines.mini]\nssh = \"mini\"\n\n[machines.lab]\nssh = \"lab\"\n")

	a, out, errOut := machineApp(t, nil, nil)
	if code := a.Run([]string{"status", "--all-machines", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var doc struct {
		Version int `json:"version"`
		Runs    []struct {
			Change, Machine, State string
		} `json:"runs"`
		Unreachable []string `json:"unreachable"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := map[string]string{}
	for _, r := range doc.Runs {
		got[r.Change] = r.Machine
	}
	if doc.Version != StatusVersion || got["add-login"] != "studio" || got["add-users"] != "mini" || len(doc.Runs) != 2 {
		t.Fatalf("status %+v", doc)
	}
	if len(doc.Unreachable) != 1 || doc.Unreachable[0] != "lab" || !strings.Contains(errOut.String(), "lab is unreachable") {
		t.Fatalf("unreachable %v, stderr %s", doc.Unreachable, errOut)
	}

	out.Reset()
	if code := a.Run([]string{"status", "--all-machines"}); code != 0 {
		t.Fatal("text form failed")
	}
	for _, want := range []string{"MACHINE", "studio", "mini", "add-login", "add-users"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text lacks %q:\n%s", want, out)
		}
	}

	// The format snapshot: status --json's version, and the fields each
	// run carries, now with its machine.
	data, err := os.ReadFile(filepath.Join("testdata", "status-all-machines.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Version int      `json:"version"`
		Fields  []string `json:"run_fields"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	_ = a.Run([]string{"status", "--all-machines", "--json"})
	var raw struct {
		Version int              `json:"version"`
		Runs    []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Version != want.Version {
		t.Fatalf("version %d", raw.Version)
	}
	for _, r := range raw.Runs {
		for _, k := range want.Fields {
			if _, ok := r[k]; !ok {
				t.Fatalf("run lacks %q: %v", k, r)
			}
		}
	}
}

// A forge token variable named in machine config ([forges.<host>]
// token_env) never reaches the transport, so ssh's SendEnv can't carry it
// to another machine; nor does any other variable named like a secret.
func TestRemoteRunnerSendsNoConfiguredToken(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	miniState := filepath.Join(f.env.Home, "mini-state")
	rec := filepath.Join(f.env.Home, "ssh-env")
	t.Setenv("FAKE_SSH_STATE_mini", miniState)
	t.Setenv("FAKE_SSH_ENV_RECORD", rec)
	t.Setenv("WORK_GITLAB_TOKEN", "glpat-secret-1")
	t.Setenv("FAKE_WORK_KEY", "kept-for-the-fake")
	t.Setenv("FAKE_DEPLOY_TOKEN", "secret-2")
	writeMachineConfig(t, f.env, "machine = \"studio\"\n\n[machines.mini]\nssh = \"mini\"\n\n[forges.\"gitlab.example.org\"]\nkind = \"gitlab\"\ntoken_env = \"WORK_GITLAB_TOKEN\"\n")
	a, _, errOut := machineApp(t, nil, nil)
	// The fake's Keep would pass any FAKE_ or configured name; Strip wins.
	a.MachineRunner.Keep = append(a.MachineRunner.Keep, "WORK_")
	if code := a.Run([]string{"status", "--all-machines", "--json"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	env := readFile(t, rec)
	for _, secret := range []string{"glpat-secret-1", "secret-2", "WORK_GITLAB_TOKEN"} {
		if strings.Contains(env, secret) {
			t.Fatalf("ssh started with %s in its environment:\n%s", secret, env)
		}
	}
	if !strings.Contains(env, "FAKE_WORK_KEY=kept-for-the-fake") || !strings.Contains(env, "PATH=") {
		t.Fatalf("ssh lacks the variables it needs:\n%s", env)
	}
}
