package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// configApp returns an App whose working directory is a fake repository
// root and whose user config directory is empty.
func configApp(t *testing.T) (App, string, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	cfgDir := t.TempDir()
	var out, errOut bytes.Buffer
	a := App{
		Stdout: &out,
		Stderr: &errOut,
		ConfigEnv: config.Env{
			Git: func(dir string, args ...string) (string, error) {
				if strings.Join(args, " ") == "rev-parse --show-toplevel" {
					return filepath.ToSlash(root) + "\n", nil
				}
				return "", errors.New("exit status 1")
			},
			Getenv: func(k string) string {
				if k == "XDG_CONFIG_HOME" {
					return cfgDir
				}
				return ""
			},
		},
		Getwd: func() (string, error) { return root, nil },
	}
	return a, root, &out, &errOut
}

func TestInitThenCheck(t *testing.T) {
	a, root, out, errOut := configApp(t)
	if code := a.Run([]string{"init"}); code != 0 {
		t.Fatalf("init exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), ".tuckerbox/config.toml") {
		t.Errorf("init output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, config.RepoFile)); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := a.Run([]string{"check"}); code != 0 {
		t.Fatalf("check exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "plan -> build -> review") {
		t.Errorf("check output: %s", out)
	}
	errOut.Reset()
	if code := a.Run([]string{"init"}); code != 1 || !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("second init: exit %d: %s", code, errOut)
	}
}

func TestCheckNoConfig(t *testing.T) {
	a, _, _, errOut := configApp(t)
	if code := a.Run([]string{"check"}); code != 1 || !strings.Contains(errOut.String(), "tuckerbox init") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

// Scenario: Loop to an undefined step, through the command.
func TestCheckReportsFileAndKey(t *testing.T) {
	a, root, _, errOut := configApp(t)
	path := filepath.Join(root, config.RepoFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := strings.Replace(config.Starter, `on_reject = "build"`, `on_reject = "bulid"`, 1)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := a.Run([]string{"check"}); code == 0 {
		t.Fatal("check passed")
	}
	want := `.tuckerbox/config.toml: steps.review.on_reject: unknown step "bulid"`
	if !strings.Contains(errOut.String(), want) {
		t.Errorf("stderr lacks %q:\n%s", want, errOut)
	}
}

// agent-launch, scenario Agent with neither: a custom agent whose command
// passes neither {brief} nor {brief_file} fails tuckerbox check; one that
// passes either is accepted.
func TestCheckAgentWithNeither(t *testing.T) {
	for _, tc := range []struct {
		command string
		ok      bool
	}{
		{`["omp", "--cwd", "{workdir}"]`, false},
		{`["omp", "{brief}"]`, true},
		{`["omp", "--prompt-file", "{brief_file}"]`, true},
	} {
		a, root, _, errOut := configApp(t)
		path := filepath.Join(root, config.RepoFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := config.Starter + "\n[agents.omp]\ncommand = " + tc.command + "\n"
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		code := a.Run([]string{"check"})
		if tc.ok {
			if code != 0 {
				t.Errorf("%s: check exit %d: %s", tc.command, code, errOut)
			}
			continue
		}
		want := "agents.omp.command: must pass {brief} (the brief's text) or {brief_file} (the brief file's path)"
		if code == 0 || !strings.Contains(errOut.String(), want) {
			t.Errorf("%s: check exit %d, stderr lacks %q:\n%s", tc.command, code, want, errOut)
		}
	}
}

func TestCheckAndInitRejectArgs(t *testing.T) {
	for _, cmd := range []string{"check", "init"} {
		a, _, _, _ := configApp(t)
		if code := a.Run([]string{cmd, "x"}); code != 2 {
			t.Errorf("%s x: exit %d", cmd, code)
		}
	}
}

// change-dependencies, scenarios Unknown dependency and Cycle: tuckerbox
// check fails, naming both changes or listing the cycle.
func TestCheckDependencies(t *testing.T) {
	a, root, out, errOut := configApp(t)
	if code := a.Run([]string{"init"}); code != 0 {
		t.Fatalf("init exit %d: %s", code, errOut)
	}
	write := func(rel, data string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("openspec/changes/b/tuckerbox.toml", "depends_on = [\"add-login\"]\n\n[approval]\nhash = \"sha256:00\"\n")
	out.Reset()
	if code := a.Run([]string{"check"}); code != 1 {
		t.Fatalf("unknown dependency: check exit %d", code)
	}
	if s := errOut.String(); !strings.Contains(s, "change b depends on add-login") || !strings.Contains(s, "1 problem") {
		t.Errorf("unknown dependency: %s", s)
	}

	write("openspec/changes/b/tuckerbox.toml", "depends_on = [\"a\"]\n")
	write("openspec/changes/a/tuckerbox.toml", "depends_on = [\"b\"]\n")
	errOut.Reset()
	if code := a.Run([]string{"check"}); code != 1 {
		t.Fatalf("cycle: check exit %d", code)
	}
	if s := errOut.String(); !strings.Contains(s, "dependency cycle: a -> b -> a") {
		t.Errorf("cycle: %s", s)
	}

	write("openspec/changes/a/tuckerbox.toml", "")
	errOut.Reset()
	if code := a.Run([]string{"check"}); code != 0 {
		t.Fatalf("clean: check exit %d: %s", code, errOut)
	}
}
