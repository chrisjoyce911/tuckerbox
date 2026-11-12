package cli

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

func app(installed ...string) (App, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	set := map[string]bool{}
	for _, n := range installed {
		set[n] = true
	}
	return App{
		Stdout: &out,
		Stderr: &errOut,
		DoctorEnv: doctor.Env{
			LookPath: func(name string) (string, error) {
				if set[name] {
					return name, nil
				}
				return "", exec.ErrNotFound
			},
			Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
				return []byte(path + " version 1.2.3\n"), nil
			},
		},
		DoctorTools: doctor.DefaultTools,
	}, &out, &errOut
}

func TestVersion(t *testing.T) {
	oldV, oldC := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = oldV, oldC })
	version.Version, version.Commit = "v0.1.0", "abc1234"

	a, out, _ := app()
	if code := a.Run([]string{"version"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, s := range []string{"tuckerbox v0.1.0", "commit: abc1234", "go: go", "platform: "} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
}

func TestVersionRejectsArgs(t *testing.T) {
	a, _, errOut := app()
	if code := a.Run([]string{"version", "extra"}); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "takes no arguments") {
		t.Errorf("stderr: %s", errOut)
	}
}

func TestDoctorExitCodes(t *testing.T) {
	cases := []struct {
		name      string
		installed []string
		want      int
	}{
		{"all", []string{"git", "openspec", "claude", "codex"}, 0},
		{"no agents", []string{"git", "openspec"}, 0},
		{"no openspec", []string{"git", "claude", "codex"}, 1},
		{"no git", []string{"openspec", "claude"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, out, _ := app(c.installed...)
			if code := a.Run([]string{"doctor"}); code != c.want {
				t.Errorf("exit %d, want %d\n%s", code, c.want, out)
			}
		})
	}
}

func TestHelpAndUnknown(t *testing.T) {
	a, out, _ := app()
	if code := a.Run([]string{"help"}); code != 0 || !strings.Contains(out.String(), "doctor") {
		t.Errorf("help: exit %d\n%s", code, out)
	}
	a, _, errOut := app()
	if code := a.Run([]string{"frobnicate"}); code != 2 || !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("unknown: exit %d\n%s", code, errOut)
	}
	a, _, _ = app()
	if code := a.Run(nil); code != 2 {
		t.Errorf("no args: exit %d", code)
	}
}
