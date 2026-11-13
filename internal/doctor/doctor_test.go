package doctor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		// The fake answers the sign-in check with the version text, which
		// says neither way.
		if r.Tool.Agent && (r.Note != Unknown || r.SignIn != Unknown) {
			t.Errorf("%s: note = %q sign-in = %q, want %q", r.Tool.Name, r.Note, r.SignIn, Unknown)
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
	if strings.Contains(out, Unknown) || strings.Contains(out, "signed") {
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

// signInEnv answers each program's sign-in command with the given output
// and error, and its version query with a version.
func signInEnv(answers map[string]struct {
	out string
	err error
}) Env {
	return Env{
		LookPath: func(name string) (string, error) {
			if _, ok := answers[name]; ok {
				return filepath.Join("fake", "bin", name), nil
			}
			return "", exec.ErrNotFound
		},
		Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			a := answers[filepath.Base(path)]
			if len(args) == 1 && args[0] == "--version" {
				return []byte("1.0.0\n"), nil
			}
			return []byte(a.out), a.err
		},
	}
}

func TestSignInStates(t *testing.T) {
	exit1 := errors.New("exit status 1")
	cases := []struct {
		name, program, out string
		err                error
		want               string
	}{
		{"claude signed in", "claude", `{"loggedIn": true, "email": "x"}`, nil, SignedIn},
		{"claude signed out", "claude", `{"loggedIn": false}`, exit1, SignedOut},
		{"claude old cli", "claude", "error: unknown command 'auth'", exit1, Unknown},
		{"codex signed in", "codex", "Logged in using ChatGPT\n", nil, SignedIn},
		{"codex signed out", "codex", "Not logged in\n", exit1, SignedOut},
		{"codex other", "codex", "Usage: codex ...", exit1, Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := signInEnv(map[string]struct {
				out string
				err error
			}{c.program: {c.out, c.err}})
			for _, r := range Check(env, AgentTools(nil)) {
				if r.Tool.Name == c.program && r.SignIn != c.want {
					t.Errorf("sign-in = %q, want %q", r.SignIn, c.want)
				}
			}
		})
	}
}

// Scenario: Unknown sign-in. A custom agent omp is installed: doctor
// reports its sign-in as unknown and still passes.
func TestUnknownSignInCustomAgent(t *testing.T) {
	env := signInEnv(map[string]struct {
		out string
		err error
	}{"git": {}, "openspec": {}, "omp-cli": {}})
	tools := append(DefaultTools[:2:2], AgentTools(map[string]string{"omp": "omp-cli"})...)
	results := Check(env, tools)
	if !OK(results) {
		t.Fatal("doctor fails with an unknown sign-in")
	}
	var omp *Result
	for i := range results {
		if results[i].Tool.Name == "omp" {
			omp = &results[i]
		}
	}
	if omp == nil || !omp.Found || omp.SignIn != Unknown {
		t.Fatalf("omp = %+v, want found with sign-in unknown", omp)
	}
	var buf bytes.Buffer
	if err := Write(&buf, results); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "omp") || !strings.Contains(buf.String(), "(unknown)") {
		t.Errorf("output lacks omp's unknown sign-in:\n%s", buf.String())
	}
}

// A sign-in check that hangs counts as unknown, after SignInTimeout.
func TestSignInTimeoutIsUnknown(t *testing.T) {
	env := Env{Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}, SignInTimeout: 50 * time.Millisecond}
	tool := DefaultTools[2]
	if got := SignIn(env, "claude", tool); got != Unknown {
		t.Fatalf("sign-in = %q, want unknown", got)
	}
}
