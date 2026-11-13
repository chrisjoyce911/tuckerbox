package machines

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// The test binary doubles as a fake ssh and hc: with FAKE_REMOTE set it
// records its arguments and environment and acts as the variable says.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_REMOTE"); mode != "" {
		fakeRemote(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeRemote(mode string) {
	if rec := os.Getenv("FAKE_RECORD"); rec != "" {
		_ = os.WriteFile(rec, []byte(strings.Join(os.Args[1:], "\n")+"\n--env--\n"+strings.Join(os.Environ(), "\n")), 0o644)
	}
	switch mode {
	case "ok":
		fmt.Print(`{"ok":true}`)
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "ssh-refused":
		fmt.Fprintln(os.Stderr, "ssh: connect to host mini port 22: Connection refused")
		os.Exit(255)
	case "fail":
		fmt.Fprintln(os.Stderr, "tuckerbox machine: broken")
		os.Exit(1)
	}
	os.Exit(3)
}

func fake(t *testing.T, mode string) (Runner, string) {
	t.Helper()
	rec := filepath.Join(t.TempDir(), "record")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "FAKE_REMOTE="+mode, "FAKE_RECORD="+rec, "GITHUB_TOKEN=secret-gh", "TUCKERBOX_FORGE_TOKEN=secret-tb")
	return Runner{SSH: exe, HC: exe, Environ: env}, rec
}

func recorded(t *testing.T, rec string) (args []string, env string) {
	t.Helper()
	data, err := os.ReadFile(rec)
	if err != nil {
		t.Fatal(err)
	}
	a, e, _ := strings.Cut(string(data), "\n--env--\n")
	return strings.Split(strings.TrimSuffix(a, "\n"), "\n"), e
}

func TestSSHCommand(t *testing.T) {
	r, rec := fake(t, "ok")
	out, err := r.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini"}, []string{"machine", "--json", "--for", "github.com/sam/app"}, time.Minute)
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("out %q err %v", out, err)
	}
	args, env := recorded(t, rec)
	want := []string{"-T", "-o", "BatchMode=yes", "mini", "--", "tuckerbox machine --json --for github.com/sam/app"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("ssh args %q, want %q", args, want)
	}
	if strings.Contains(env, "secret-") {
		t.Fatalf("a token reached ssh's environment:\n%s", env)
	}
}

func TestSSHQuotesArguments(t *testing.T) {
	r := Runner{SSH: "ssh"}
	argv, err := r.Command(config.MachineEntry{Name: "mini", SSH: "mini"}, []string{"run", "a b", "it's", "$(x)"})
	if err != nil {
		t.Fatal(err)
	}
	if got := argv[len(argv)-1]; got != `tuckerbox run 'a b' 'it'\''s' '$(x)'` {
		t.Fatalf("remote command %q", got)
	}
}

// Scenario: Machine reached by herdr-core.
func TestMachineReachedByHerdrCore(t *testing.T) {
	r, rec := fake(t, "ok")
	if _, err := r.Run(context.Background(), config.MachineEntry{Name: "lab", HC: "lab"}, []string{"status", "--json"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	args, env := recorded(t, rec)
	if strings.Join(args, " ") != "run --on lab --rm -- tuckerbox status --json" {
		t.Fatalf("hc args %q", args)
	}
	if strings.Contains(env, "secret-") {
		t.Fatal("a token reached hc's environment")
	}
}

// Scenario: Both ways given. The runner refuses; config check names the
// machine (see package config).
func TestBothWaysGiven(t *testing.T) {
	_, err := Runner{}.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini", HC: "mini"}, nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "machines.mini") {
		t.Fatalf("err %v", err)
	}
}

// Scenario: Machine asleep. A machine that doesn't answer in time is
// named as unreachable.
func TestMachineAsleep(t *testing.T) {
	r, _ := fake(t, "sleep")
	start := time.Now()
	_, err := r.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini"}, []string{"machine", "--json"}, 300*time.Millisecond)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "mini") {
		t.Fatalf("err %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("the timeout did not end the command")
	}
}

func TestConnectionRefusedIsUnreachable(t *testing.T) {
	r, _ := fake(t, "ssh-refused")
	_, err := r.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini"}, nil, time.Minute)
	if !errors.Is(err, ErrUnreachable) || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatalf("err %v", err)
	}
}

func TestRemoteFailureIsNotUnreachable(t *testing.T) {
	r, _ := fake(t, "fail")
	_, err := r.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini"}, nil, time.Minute)
	var re *Error
	if errors.Is(err, ErrUnreachable) || !errors.As(err, &re) || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err %v", err)
	}
}

func TestMissingProgramIsUnreachable(t *testing.T) {
	r := Runner{SSH: filepath.Join(t.TempDir(), "no-ssh")}
	_, err := r.Run(context.Background(), config.MachineEntry{Name: "mini", SSH: "mini"}, nil, time.Minute)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err %v", err)
	}
}

func TestReportRoundTrip(t *testing.T) {
	var a AgentReport
	a.SetSignIn(SignedOut)
	rep := Report{Format: ReportVersion, Name: "mini", FleetCapacity: 2, FleetRuns: 1, Agents: []AgentReport{a}}
	if rep.Free() != 1 {
		t.Fatal("Free")
	}
	data := []byte(`{"format":1,"name":"mini","version":"1.2.0","os":"linux","fleet_capacity":2,"fleet_runs":3,"agents":[{"name":"codex","installed":true,"signed_in":false,"sign_in":"signed out"},{"name":"omp","installed":true,"signed_in":null,"sign_in":"unknown"}],"checkout":false,"runs":[]}`)
	got, err := ParseReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Free() != 0 {
		t.Fatal("over capacity must be 0 free")
	}
	codex, _ := got.Agent("codex")
	omp, _ := got.Agent("omp")
	if codex.SignedIn == nil || *codex.SignedIn || omp.SignedIn != nil {
		t.Fatalf("agents %+v %+v", codex, omp)
	}
	if _, err := ParseReport([]byte(`{"format":9,"name":"mini"}`)); err == nil {
		t.Fatal("a newer format must be refused")
	}
}
