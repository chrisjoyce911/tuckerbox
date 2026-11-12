package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

func TestShippingModesAndDefaults(t *testing.T) {
	for _, mode := range []string{"mr", "merge"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "ship = \""+mode+"\"\n"+minimal, "")
			f.remote = "https://github.com/sam/app.git"
			c := f.mustLoad()
			if string(c.Ship) != mode || c.Shipping.Remote != "origin" || c.Shipping.MaxRounds != 3 || !c.Shipping.Archive || c.Shipping.WaitTimeout != 2*time.Hour || c.ForgeTarget.APIURL != "https://api.github.com" {
				t.Fatalf("%+v", c)
			}
		})
	}
	for _, text := range []string{`ship = "mr"`, `ship = "merge"`, `[shipping]`, `[forge]`} {
		f := newFixture(t, minimal, text)
		err := f.problems().Error()
		if !strings.Contains(err, "repository config") {
			t.Fatal(err)
		}
	}
	f := newFixture(t, minimal, `ship = "local"`)
	if f.mustLoad().Ship != ShipLocal {
		t.Fatal("local default changed")
	}
}
func TestShippingFixSteps(t *testing.T) {
	extra := `[shipping]
fix_ci = "fix_ci"
max_rounds = 4
archive = false
labels = ["automation"]
review_from = ["review-bot"]
merge_method = "squash"
wait_timeout = "1h"
[steps.fix_ci]
agent = "claude"
role = "fixer"
`
	f := newFixture(t, minimal+extra, `machine = "studio"`)
	c := f.mustLoad()
	s, ok := c.Step("fix_ci")
	if !ok || s.Role != "fixer" || len(c.Steps) != 3 || c.Shipping.FixReview != "fix_ci" || c.Shipping.Archive || c.Shipping.MaxRounds != 4 || c.Shipping.MergeMethod != "squash" || c.Machine != "studio" {
		t.Fatal(c)
	}
	for _, tt := range []struct{ text, key, fragment string }{
		{"[shipping]\nfix_ci='fixci'", "shipping.fix_ci", "unknown step"},
		{"[shipping]\nfix_ci='build'", "shipping.fix_ci", "workflow.steps"},
		{"[shipping]\nfix_review='review'", "shipping.fix_review", "workflow.steps"},
		{"[shipping]\nmax_rounds=0", "shipping.max_rounds", "at least 1"},
		{"[shipping]\nwait_timeout='0s'", "shipping.wait_timeout", "positive duration"},
		{"[shipping]\nmerge_method='fast'", "shipping.merge_method", "merge, squash or rebase"},
		{"[shipping]\nremote='-oops'", "shipping.remote", "git remote"},
	} {
		f := newFixture(t, minimal+tt.text, "")
		wantProblem(t, f.problems(), f.repoPath(), tt.key, tt.fragment)
	}
}
func TestRepositoryTokensAreRejectedWithoutValues(t *testing.T) {
	for _, text := range []string{
		`token = "secret-value"`,
		`token_env = "secret-value"`,
		`borrow_cli_token = true`,
		`[forge]
token = "secret-value"`,
		`[forge]
token_env = "secret-value"`,
		`[forge]
borrow_cli_token = "secret-value"`,
		`[forges."gitlab.com"]
token_env = "secret-value"`,
	} {
		f := newFixture(t, minimal+text, "")
		ps := f.problems()
		if strings.Contains(ps.Error(), "secret-value") || !strings.Contains(ps.Error(), "machine config") {
			t.Fatal(ps)
		}
	}
}
func TestShippingForgeValidationAndPushRemote(t *testing.T) {
	f := newFixture(t, "ship = 'mr'\n"+minimal+"[shipping]\nremote='publish'", `[forges."git.example.org"]
kind = "gitlab"
token_env = "WORK_TOKEN"
borrow_cli_token = true
`)
	cfg, err := Load(Options{RepoRoot: f.root, MachinePath: f.machine, Remote: "https://github.com/sam/app", PushURL: func(remote string) (string, error) {
		if remote != "publish" {
			t.Fatal(remote)
		}
		return "https://git.example.org/team/tools/app.git", nil
	}})
	if err != nil || cfg.ForgeTarget.Kind != forge.GitLab || cfg.ForgeTarget.Project != "team/tools/app" || cfg.ForgeTarget.APIURL != "https://git.example.org/api/v4" || cfg.Forges["git.example.org"].TokenEnv != "WORK_TOKEN" {
		t.Fatal(cfg, err)
	}
	for _, tt := range []struct{ remote, settings, want string }{
		{"https://unknown.example/team/app", "", "[forge] kind"},
		{"https://git.example.org/team/app", "[forge]\nkind='gitlab'\napi_url='https://evil.example/api'", "remote's host"},
		{"https://git.example.org/team/app", "[forge]\nkind='gitlab'\napi_url='http://git.example.org/api'", "HTTPS"},
	} {
		f := newFixture(t, "ship='mr'\n"+minimal+tt.settings, "")
		f.remote = tt.remote
		if err := f.problems().Error(); !strings.Contains(err, tt.want) {
			t.Fatal(err)
		}
	}
}
func TestPushURLUsesEffectivePushURL(t *testing.T) {
	// A local git repository needs no forge or network to test URL selection.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	env := SystemEnv()
	if _, err := env.Git(root, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Git(root, "remote", "add", "origin", "https://github.com/read/app.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Git(root, "remote", "set-url", "--push", "origin", "https://gitlab.com/write/app.git"); err != nil {
		t.Fatal(err)
	}
	got, err := forge.PushURL(context.Background(), root, "")
	if err != nil || got != "https://gitlab.com/write/app.git" {
		t.Fatal(got, err)
	}
}
func TestREADMEConfigExamples(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	found := 0
	for _, part := range strings.Split(text, "```toml\n")[1:] {
		example, _, _ := strings.Cut(part, "```")
		if strings.Contains(example, "ship = \"mr\"") {
			f := newFixture(t, example, "")
			f.remote = "https://github.com/sam/app"
			f.mustLoad()
			found++
		}
		if strings.Contains(example, "[forges.") {
			f := newFixture(t, minimal, example)
			f.mustLoad()
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected repository and machine shipping examples, found %d", found)
	}
}
