package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// machineConfig writes the test's machine config (XDG_CONFIG_HOME is under
// home; see testenv.Setup).
func machineConfig(t *testing.T, home, text string) {
	t.Helper()
	testenv.WriteFile(t, filepath.Join(home, "config", "tuckerbox", "config.toml"), text)
}

// Scenario: No herdr-core. agent_backend is not set and hc is not
// installed: agents run headless as before, and doctor says the backend is
// headless because herdr-core wasn't found.
func TestNoHerdrCore(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	t.Setenv("PATH", testenv.PathWithout(os.Getenv("PATH"), "hc"))

	res := f.tb("doctor")
	if want := "agent backend: headless, because herdr-core wasn't found (no hc on the PATH)"; !strings.Contains(res.stdout, want) {
		t.Errorf("doctor lacks %q:\n%s%s", want, res.stdout, res.stderr)
	}
	res = f.tb("run", "--foreground", "fix-login")
	if res.code != 0 {
		t.Fatalf("run exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	for _, s := range []string{"step build round 1: starting agent", "(headless)"} {
		if !strings.Contains(res.stdout, s) {
			t.Errorf("run output lacks %q:\n%s", s, res.stdout)
		}
	}
	if s, _ := f.state("fix-login"); s != "ready" {
		t.Errorf("state %s", s)
	}
	if got := f.env.Starts(t); len(got) != 2 {
		t.Errorf("agent starts %v, want build and review as child processes", got)
	}
}

// Scenario: Terminal asked for without herdr-core. Machine config sets
// agent_backend = "terminal" and `hc server status` fails: tuckerbox run
// refuses, says herdr-core isn't running, and creates no run state.
func TestTerminalWithoutHerdrCore(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	machineConfig(t, f.env.Home, "agent_backend = \"terminal\"\n")
	t.Setenv("PATH", tools.HCBin+string(os.PathListSeparator)+testenv.PathWithout(os.Getenv("PATH"), "hc"))
	t.Setenv("FAKEHC_SERVER", "down")

	res := f.tb("run", "fix-login")
	if res.code != 1 || !strings.Contains(res.stderr, "herdr-core isn't running") {
		t.Errorf("run: exit %d\n%s", res.code, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.repo), "repo-worktrees")); err == nil {
		t.Error("worktree directory made")
	}
	if out := testenv.Git(t, f.repo, "branch", "--list", "fix-login"); out != "" {
		t.Errorf("branch made: %s", out)
	}
	if len(f.status().Runs) != 0 {
		t.Error("run recorded")
	}
	if got := f.env.Starts(t); len(got) != 0 {
		t.Errorf("agent started: %v", got)
	}
	res = f.tb("doctor")
	if !strings.Contains(res.stdout, "agent backend: none, runs refuse to start") || !strings.Contains(res.stdout, "herdr-core isn't running") {
		t.Errorf("doctor:\n%s", res.stdout)
	}
}

// doctor names the reason for each choice.
func TestDoctorBackendLine(t *testing.T) {
	cases := []struct {
		name, machine string
		installed     []string
		want          string
	}{
		{"no hc", "", nil, "agent backend: headless, because herdr-core wasn't found (no hc on the PATH)"},
		{"hc running", "", []string{"hc"}, "agent backend: terminal, because herdr-core is running"},
		{"headless", "agent_backend = \"headless\"\n", []string{"hc"}, `agent backend: headless, because agent_backend is "headless" in machine config`},
		{"terminal, no hc", "agent_backend = \"terminal\"\n", nil, "agent backend: none, runs refuse to start: agent_backend is \"terminal\" in machine config, but herdr-core wasn't found"},
		{"bad value", "agent_backend = \"tmux\"\n", nil, "agent backend: can't read machine config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfg)
			if c.machine != "" {
				testenv.WriteFile(t, filepath.Join(cfg, "tuckerbox", "config.toml"), c.machine)
			}
			a, out, _ := app(append([]string{"git", "openspec"}, c.installed...)...)
			a.Getwd = func() (string, error) { return cfg, nil }
			a.Run([]string{"doctor"})
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("doctor lacks %q:\n%s", c.want, out)
			}
		})
	}
}
