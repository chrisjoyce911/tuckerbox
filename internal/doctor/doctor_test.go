package doctor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEnv finds the tools in installed and answers version queries with
// their output. Paths are built with filepath so the test runs on any OS.
func fakeEnv(installed map[string]string) Env {
	dir := filepath.Join("fake", "bin")
	return Env{
		LookPath: func(name string) (string, error) {
			if _, ok := installed[name]; ok {
				return filepath.Join(dir, name), nil
			}
			return "", exec.ErrNotFound
		},
		Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			out, ok := installed[filepath.Base(path)]
			if !ok {
				return nil, errors.New("not installed")
			}
			if out == "<fail>" {
				return []byte("boom"), errors.New("exit status 1")
			}
			return []byte(out), nil
		},
	}
}

func TestCheckAllFound(t *testing.T) {
	env := fakeEnv(map[string]string{
		"git":      "git version 2.43.0\n",
		"openspec": "1.16.0\n",
		"claude":   "2.1.3 (Claude Code)\n",
		"codex":    "codex-cli 0.50.0\n",
	})
	results := Check(env, DefaultTools)
	if !OK(results) {
		t.Fatal("OK = false, want true")
	}
	want := map[string]string{"git": "2.43.0", "openspec": "1.16.0", "claude": "2.1.3", "codex": "0.50.0"}
	for _, r := range results {
		if !r.Found || r.Version != want[r.Tool.Name] {
			t.Errorf("%s: found=%v version=%q, want %q", r.Tool.Name, r.Found, r.Version, want[r.Tool.Name])
		}
		if r.Tool.Agent && r.Note != SignInNotChecked {
			t.Errorf("%s: note = %q, want %q", r.Tool.Name, r.Note, SignInNotChecked)
		}
		if !r.Tool.Agent && r.Note != "" {
			t.Errorf("%s: unexpected note %q", r.Tool.Name, r.Note)
		}
	}
}

func TestMissingOptionalAgentIsOK(t *testing.T) {
	env := fakeEnv(map[string]string{"git": "git version 2.43.0", "openspec": "1.16.0"})
	results := Check(env, DefaultTools)
	if !OK(results) {
		t.Fatal("missing agents must not fail doctor")
	}
	var buf bytes.Buffer
	if err := Write(&buf, results); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, s := range []string{"claude", "codex", "missing", "all required tools found"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q:\n%s", s, out)
		}
	}
	if strings.Contains(out, SignInNotChecked) {
		t.Errorf("sign-in note shown for a missing agent:\n%s", out)
	}
}

func TestMissingRequiredFails(t *testing.T) {
	for _, name := range []string{"git", "openspec"} {
		t.Run(name, func(t *testing.T) {
			installed := map[string]string{"git": "git version 2.43.0", "openspec": "1.16.0", "claude": "2.1.3"}
			delete(installed, name)
			results := Check(fakeEnv(installed), DefaultTools)
			if OK(results) {
				t.Fatalf("OK = true with %s missing", name)
			}
			var buf bytes.Buffer
			if err := Write(&buf, results); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(buf.String(), "missing required tools: "+name) {
				t.Errorf("output:\n%s", buf.String())
			}
		})
	}
}

func TestVersionFailureStillFound(t *testing.T) {
	env := fakeEnv(map[string]string{"git": "<fail>", "openspec": "1.16.0"})
	results := Check(env, DefaultTools[:2])
	if !results[0].Found || results[0].Version != "" {
		t.Errorf("git result = %+v", results[0])
	}
	var buf bytes.Buffer
	if err := Write(&buf, results); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "version unknown") {
		t.Errorf("output:\n%s", buf.String())
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"git version 2.43.0\n":               "2.43.0",
		"git version 2.39.3 (Apple Git-145)": "2.39.3",
		"git version 2.45.1.windows.1\r\n":   "2.45.1.windows.1",
		"1.0.33 (Claude Code)":               "1.0.33",
		"codex-cli 0.50.0":                   "0.50.0",
		"v1.2.3-rc.1":                        "v1.2.3-rc.1",
		"\n\nsome tool, no number\n":         "some tool, no number",
		"":                                   "",
	}
	for in, want := range cases {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSystemEnvFindsGoTool runs the real lookup against a tool the test
// itself needs, so it works on every OS the tests run on.
func TestSystemEnvFindsGoTool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	r := Check(SystemEnv(), []Tool{{Name: "go", VersionArgs: []string{"version"}}})[0]
	if !r.Found || r.Version == "" {
		t.Errorf("go result = %+v", r)
	}
	r = Check(SystemEnv(), []Tool{{Name: "tuckerbox-no-such-tool-x9", Required: true}})[0]
	if r.Found {
		t.Errorf("found a tool that does not exist: %+v", r)
	}
}
