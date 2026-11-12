package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

const showWorkflow = `
[workflow]
steps = ["plan", "build", "review"]

[steps.plan]
agent = "claude"
role = "planner"

[steps.build]
agent = "codex"
role = "builder"

[steps.review]
agent = "claude"
role = "reviewer"
on_reject = "build"
`

// showApp is configApp with the given repository and machine config.
func showApp(t *testing.T, repo, machine string) (App, string, string) {
	t.Helper()
	a, root, _, _ := configApp(t)
	write := func(path, data string, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, config.RepoFile), repo, 0o644)
	machinePath := filepath.Join(a.ConfigEnv.Getenv("XDG_CONFIG_HOME"), "tuckerbox", "config.toml")
	if machine != "" {
		write(machinePath, machine, 0o644)
	}
	a.BorrowToken = func(context.Context, string, ...string) (string, error) {
		return "", errors.New("no borrowing in tests")
	}
	return a, root, machinePath
}

func runOut(t *testing.T, a App, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut strings.Builder
	a.Stdout, a.Stderr = &out, &errOut
	code := a.Run(args)
	return code, out.String(), errOut.String()
}

func showJSON(t *testing.T, a App, args ...string) jsonConfig {
	t.Helper()
	code, out, errOut := runOut(t, a, append(args, "--json")...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got jsonConfig
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return got
}

func findKey(t *testing.T, c jsonConfig, key string) jsonConfigKey {
	t.Helper()
	for _, k := range c.Keys {
		if k.Key == key {
			return k
		}
	}
	t.Fatalf("no key %s", key)
	return jsonConfigKey{}
}

// Scenario: Repository value wins.
func TestConfigShowRepositoryValueWins(t *testing.T) {
	a, _, _ := showApp(t, "max_parallel = 2\n"+showWorkflow, "max_parallel = 4\n")
	code, out, errOut := runOut(t, a, "config", "show")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "max_parallel = 2 (repo; machine sets 4) [either]") {
		t.Fatalf("show:\n%s", out)
	}
	k := findKey(t, showJSON(t, a, "config", "show"), "max_parallel")
	if k.Value != float64(2) || k.Source != "repo" || k.Scope != "either" || k.File != "repo" || k.Type != "int" ||
		k.Overridden == nil || k.Overridden.Value != float64(4) || k.Overridden.Source != "machine" {
		t.Fatalf("json: %+v", k)
	}
}

// Scenario: Built-in default.
func TestConfigShowBuiltinDefault(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "")
	_, out, _ := runOut(t, a, "config", "show")
	if !strings.Contains(out, "board.columns = compact (default)") {
		t.Fatalf("show:\n%s", out)
	}
	k := findKey(t, showJSON(t, a, "config", "show"), "board.columns")
	if k.Value != "compact" || k.Source != "default" || k.File != "" || k.Type != "string" || len(k.Allowed) != 2 {
		t.Fatalf("json: %+v", k)
	}
}

// Scenario: Permission profile.
func TestConfigShowPermissionProfile(t *testing.T) {
	machine := "machine = \"studio\"\n[permissions.edit]\nallowed_remotes = [\"*\"]\nargs.claude = [\"--permission-mode\", \"acceptEdits\"]\n"
	a, _, _ := showApp(t, showWorkflow, machine)
	_, out, _ := runOut(t, a, "config", "show")
	head := strings.Index(out, "studio: this machine only")
	key := strings.Index(out, "permissions.edit.allowed_remotes = * (machine)")
	if head < 0 || key < head {
		t.Fatalf("permissions.edit not under this machine's heading:\n%s", out)
	}
	c := showJSON(t, a, "config", "show")
	if c.Machine != "studio" || findKey(t, c, "permissions.edit.allowed_remotes").Scope != "machine" {
		t.Fatalf("json: %+v", c)
	}
}

// Scenario: Revision for a later save.
func TestConfigShowRevisions(t *testing.T) {
	repo := showWorkflow
	a, _, _ := showApp(t, repo, "")
	c := showJSON(t, a, "config", "show")
	if r := c.Files["repo"].Revision; r == nil || *r != config.Revision([]byte(repo)) {
		t.Fatalf("repo revision %v", r)
	}
	if c.Files["machine"].Revision != nil || c.Files["machine"].Path == "" {
		t.Fatalf("absent machine file: %+v", c.Files["machine"])
	}
}

// Scenario: Step setting.
func TestConfigGetStepSetting(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow+"effort = \"high\"\n", "")
	code, out, errOut := runOut(t, a, "config", "get", "steps.review.effort")
	if code != 0 || out != "high (repo)\n" {
		t.Fatalf("exit %d: %q %s", code, out, errOut)
	}
	_, out, _ = runOut(t, a, "config", "get", "steps.review")
	for _, want := range []string{"steps.review.agent = claude (repo)", "steps.review.effort = high (repo)", "steps.review.max_rounds = 3 (default)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("get table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "steps.plan") {
		t.Fatalf("other table shown:\n%s", out)
	}
	c := showJSON(t, a, "config", "get", "steps.review.effort")
	if len(c.Keys) != 1 || c.Keys[0].Value != "high" {
		t.Fatalf("get json: %+v", c.Keys)
	}
}

// Scenario: Misspelt key.
func TestConfigGetMisspeltKey(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "")
	code, _, errOut := runOut(t, a, "config", "get", "max_paralel")
	if code == 0 || !strings.Contains(errOut, "max_parallel") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// Scenario: Token set.
func TestConfigShowTokenSet(t *testing.T) {
	const secret = "ghp_tokenvalue123"
	a, _, machinePath := showApp(t, showWorkflow, "")
	creds := config.CredentialsPath(machinePath)
	if err := os.MkdirAll(filepath.Dir(creds), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds, []byte("[forges.\"github.com\"]\ntoken = \""+secret+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runOut(t, a, "config", "show")
	if !strings.Contains(out, `forges."github.com".token = set (credentials file)`) {
		t.Fatalf("show:\n%s", out)
	}
	_, jsonOut, _ := runOut(t, a, "config", "show", "--json")
	_, getOut, _ := runOut(t, a, "config", "get", `forges."github.com".token`, "--json")
	for _, s := range []string{out, jsonOut, getOut} {
		if strings.Contains(s, secret) || strings.Contains(s, secret[4:12]) {
			t.Fatalf("token in output:\n%s", s)
		}
	}
	k := findKey(t, showJSON(t, a, "config", "show"), `forges."github.com".token`)
	if k.Value != "set" || !k.Set || k.Source != "credentials" || !k.Secret || k.File != "credentials" {
		t.Fatalf("json: %+v", k)
	}
}

func TestConfigShowTokenFromEnv(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "[forges.\"github.com\"]\ntoken_env = \"MY_TOKEN\"\n")
	getenv := a.ConfigEnv.Getenv
	a.ConfigEnv.Getenv = func(k string) string {
		if k == "MY_TOKEN" {
			return "env-secret-value"
		}
		return getenv(k)
	}
	_, out, _ := runOut(t, a, "config", "show", "--json")
	if strings.Contains(out, "env-secret-value") {
		t.Fatal("token printed")
	}
	k := findKey(t, showJSON(t, a, "config", "show"), `forges."github.com".token`)
	if k.Value != "set" || k.Source != "env:MY_TOKEN" {
		t.Fatalf("json: %+v", k)
	}
}

// Scenario: Token in machine config, through check.
func TestCheckTokenInMachineConfig(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "[forges.\"github.com\"]\ntoken = \"machine-secret\"\n")
	code, out, errOut := runOut(t, a, "check")
	if code == 0 || !strings.Contains(errOut, "credentials file") || strings.Contains(out+errOut, "machine-secret") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
}

// Scenario: File readable by others, through check.
func TestCheckCredentialsReadableByOthers(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("file modes do not apply on Windows")
	}
	a, _, machinePath := showApp(t, showWorkflow, "")
	creds := config.CredentialsPath(machinePath)
	if err := os.MkdirAll(filepath.Dir(creds), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds, []byte("[forges.\"github.com\"]\ntoken = \"world-secret\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runOut(t, a, "check")
	if code == 0 || !strings.Contains(errOut, creds) || !strings.Contains(errOut, "chmod 600") || strings.Contains(out+errOut, "world-secret") {
		t.Fatalf("exit %d: %s%s", code, out, errOut)
	}
}

func TestConfigShowBrokenFile(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "max_parallel = \n")
	c := showJSON(t, a, "config", "show")
	if e := c.Files["machine"].Error; e == nil || e.Line != 1 {
		t.Fatalf("machine error: %+v", c.Files["machine"])
	}
	if findKey(t, c, "steps.review.on_reject").Value != "build" {
		t.Fatal("repo values missing")
	}
}

func TestConfigUsage(t *testing.T) {
	a, _, _ := showApp(t, showWorkflow, "")
	for _, args := range [][]string{{"config"}, {"config", "nope"}, {"config", "show", "x"}, {"config", "get"}} {
		if code, _, _ := runOut(t, a, args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}
