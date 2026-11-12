package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/configsave"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func configSaveApp(t *testing.T) (App, string, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	f := newCLI(t, testenv.RepoOptions{})
	var out, errout bytes.Buffer
	return App{Stdout: &out, Stderr: &errout, Getwd: func() (string, error) { return f.repo, nil }}, f.repo, &out, &errout
}
func configResult(t *testing.T, out *bytes.Buffer) configsave.Result {
	t.Helper()
	var r configsave.Result
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("JSON result %s: %v", out, err)
	}
	return r
}
func TestConfigSetGroupedAndScope(t *testing.T) {
	a, root, out, errout := configSaveApp(t)
	path := filepath.Join(root, config.RepoFile)
	before := readFile(t, path)
	if code := a.Run([]string{"config", "set", "--local", "--json", "max_parallel=3", "steps.review.max_rounds=2"}); code != 0 {
		t.Fatalf("%d %s %s", code, out, errout)
	}
	res := configResult(t, out)
	if !res.OK || res.Target != "local" || !strings.Contains(res.Message, "nothing was committed") {
		t.Fatalf("%+v", res)
	}
	after := readFile(t, path)
	if !strings.Contains(after, "max_parallel = 3") || !strings.Contains(after, "max_rounds = 2") {
		t.Fatal(after)
	}
	head := testenv.Git(t, root, "show", "HEAD:"+filepath.ToSlash(config.RepoFile))
	if head != strings.TrimSpace(before) {
		t.Fatal("local save committed")
	}
	for _, args := range [][]string{{"permissions.edit.allowed_remotes=*"}, {"--machine", "require_approval=false"}} {
		out.Reset()
		errout.Reset()
		cmd := append([]string{"config", "set", "--json"}, args...)
		if a.Run(cmd) == 0 {
			t.Fatal("wrong scope accepted")
		}
		res = configResult(t, out)
		if res.Error == nil || res.Error.Code != "wrong_file" {
			t.Fatalf("%+v", res)
		}
		if got := readFile(t, path); got != after {
			t.Fatal("wrong scope wrote file")
		}
	}
}
func TestConfigDryRunAndIfMatch(t *testing.T) {
	a, root, out, errout := configSaveApp(t)
	path := filepath.Join(root, config.RepoFile)
	before := readFile(t, path)
	if a.Run([]string{"config", "set", "--local", "--dry-run", "--json", "max_parallel=4"}) != 0 {
		t.Fatalf("%s %s", out, errout)
	}
	r := configResult(t, out)
	if !r.DryRun || !r.OK || readFile(t, path) != before {
		t.Fatal("dry run wrote")
	}
	out.Reset()
	if a.Run([]string{"config", "set", "--local", "--json", "--if-match", "outdated", "max_parallel=4"}) == 0 {
		t.Fatal("stale save accepted")
	}
	r = configResult(t, out)
	if r.Error == nil || r.Error.Code != "changed_since_read" || readFile(t, path) != before {
		t.Fatalf("%+v", r)
	}
}
func TestConfigUnsetUsesMachineDefault(t *testing.T) {
	a, root, out, errout := configSaveApp(t)
	env, _ := a.configEnv()
	mp, err := config.MachinePath(env)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, mp, "[steps.review]\neffort = \"high\"\n")
	path := filepath.Join(root, config.RepoFile)
	testenv.WriteFile(t, path, readFile(t, path)+"effort = \"low\"\n")
	if a.Run([]string{"config", "set", "--local", "--unset", "steps.review.effort"}) != 0 {
		t.Fatalf("%s %s", out, errout)
	}
	opts, err := config.Discover(env, root)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(opts)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := cfg.Step("review")
	if s.Effort != "high" {
		t.Fatalf("effort %s", s.Effort)
	}
}
func TestConfigTokenInput(t *testing.T) {
	a, _, out, errout := configSaveApp(t)
	a.Stdin = strings.NewReader("sensitive-token\n")
	if a.Run([]string{"config", "set", "--machine", "--json", `forges."github.com".token`}) != 0 {
		t.Fatalf("%s %s", out, errout)
	}
	if strings.Contains(out.String()+errout.String(), "sensitive-token") {
		t.Fatal("token leaked")
	}
	env, _ := a.configEnv()
	mp, err := config.MachinePath(env)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, config.CredentialsPath(mp)), "sensitive-token") {
		t.Fatal("token not stored")
	}
	if _, err = os.Stat(mp); !os.IsNotExist(err) {
		t.Fatal("wrote config.toml")
	}
	out.Reset()
	errout.Reset()
	if a.Run([]string{"config", "set", "--machine", "--json", `forges.github.com.token=argument-secret`}) == 0 {
		t.Fatal("argument accepted")
	}
	if strings.Contains(out.String()+errout.String(), "argument-secret") {
		t.Fatal("argument leaked")
	}
	if !strings.Contains(errout.String(), "leave out the token value") {
		t.Fatal(errout)
	}
	out.Reset()
	if a.Run([]string{"config", "set", "--machine", "--unset", `forges."github.com".token`}) != 0 {
		t.Fatal(errout)
	}
	if strings.Contains(readFile(t, config.CredentialsPath(mp)), "sensitive-token") {
		t.Fatal("token not removed")
	}
	opts, err := config.Discover(env, aGetwd(t, a))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(opts); err != nil {
		t.Fatalf("unset left invalid credentials: %v", err)
	}
}
func TestConfigLocalResolvesMainCheckout(t *testing.T) {
	a, root, _, errout := configSaveApp(t)
	wt := filepath.Join(t.TempDir(), "change")
	testenv.Git(t, root, "worktree", "add", "-b", "change", wt)
	a.Getwd = func() (string, error) { return wt, nil }
	old := readFile(t, filepath.Join(wt, config.RepoFile))
	if a.Run([]string{"config", "set", "--local", "max_parallel=8"}) != 0 {
		t.Fatal(errout)
	}
	if readFile(t, filepath.Join(wt, config.RepoFile)) != old {
		t.Fatal("modified worktree")
	}
	if !strings.Contains(readFile(t, filepath.Join(root, config.RepoFile)), "max_parallel = 8") {
		t.Fatal("did not modify main checkout")
	}
}
func TestConfigEditChecksSyntax(t *testing.T) {
	a, root, _, errout := configSaveApp(t)
	editor := "'" + fakeEditor(t) + "' '--append' '[broken'"
	env, _ := a.configEnv()
	mp, err := config.MachinePath(env)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, mp, "editor = "+strconv.Quote(editor)+"\n")
	if a.Run([]string{"config", "edit"}) == 0 {
		t.Fatal("invalid TOML passed")
	}
	if !strings.Contains(errout.String(), "config.toml:") {
		t.Fatal(errout)
	}
	if !strings.Contains(readFile(t, filepath.Join(root, config.RepoFile)), "[broken") {
		t.Fatal("editor did not run")
	}
}
func TestConfigDefaultNoTokenAndRequiredPR(t *testing.T) {
	a, root, out, errout := configSaveApp(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, root, "init", "--bare", bare)
	testenv.Git(t, root, "remote", "add", "origin", bare)
	testenv.Git(t, root, "push", "origin", "main")
	t.Setenv(config.TestForgeURLEnv, "https://github.com/test/shop.git")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if a.Run([]string{"config", "set", "--json", "max_parallel=3"}) != 0 {
		t.Fatalf("%s %s", out, errout)
	}
	r := configResult(t, out)
	if r.Target != "local" || !strings.Contains(r.Reason, "no token for github.com") {
		t.Fatalf("%+v", r)
	}
	after := readFile(t, filepath.Join(root, config.RepoFile))
	out.Reset()
	if a.Run([]string{"config", "set", "--pr", "--json", "max_parallel=8"}) == 0 {
		t.Fatal("required PR fell back")
	}
	if readFile(t, filepath.Join(root, config.RepoFile)) != after {
		t.Fatal("required PR wrote locally")
	}
}

type configCLIForge struct {
	forge.Client
	opened int
}

func (f *configCLIForge) Access(context.Context) (forge.Access, error) {
	return forge.Access{Read: true, Push: true, DefaultBranch: "main"}, nil
}
func (f *configCLIForge) OpenMR(context.Context, forge.OpenRequest) (*forge.MR, error) {
	f.opened++
	return &forge.MR{URL: "https://github.com/test/shop/pull/1"}, nil
}
func TestConfigDefaultTokenUsesPR(t *testing.T) {
	a, root, out, errout := configSaveApp(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, root, "init", "--bare", bare)
	testenv.Git(t, root, "remote", "add", "origin", bare)
	testenv.Git(t, root, "push", "origin", "main")
	t.Setenv(config.TestForgeURLEnv, "https://github.com/test/shop.git")
	t.Setenv("GH_TOKEN", "fake-token")
	f := &configCLIForge{}
	a.Forge = func(context.Context, *config.Config) (forge.Client, func(string) string, error) {
		return f, func(s string) string { return s }, nil
	}
	before := readFile(t, filepath.Join(root, config.RepoFile))
	if a.Run([]string{"config", "set", "--json", "max_parallel=8"}) != 0 {
		t.Fatalf("%s %s", out, errout)
	}
	r := configResult(t, out)
	if r.Target != "pr" || r.Link == "" || f.opened != 1 {
		t.Fatalf("%+v", r)
	}
	if readFile(t, filepath.Join(root, config.RepoFile)) != before {
		t.Fatal("PR modified checkout")
	}
}

func aGetwd(t *testing.T, a App) string {
	t.Helper()
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
