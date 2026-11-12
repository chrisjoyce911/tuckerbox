package config

import (
	"slices"
	"strings"
	"testing"
)

const allowList = `
[overrides]
agents = ["claude", "codex"]
models = { claude = ["opus", "sonnet"], codex = ["gpt-5.5"] }
efforts = ["medium", "high"]
permissions = ["edit", "readonly"]
`

func TestOverridesParsed(t *testing.T) {
	cfg := newFixture(t, minimal+allowList, "").mustLoad()
	o := cfg.Overrides
	if o == nil {
		t.Fatal("no overrides")
	}
	if !slices.Equal(o.Agents, []string{"claude", "codex"}) ||
		!slices.Equal(o.Models["claude"], []string{"opus", "sonnet"}) ||
		!slices.Equal(o.Models["codex"], []string{"gpt-5.5"}) ||
		!slices.Equal(o.Efforts, []string{"medium", "high"}) ||
		!slices.Equal(o.Permissions, []string{"edit", "readonly"}) {
		t.Errorf("overrides: %+v", o)
	}
	if cfg := newFixture(t, minimal, "").mustLoad(); cfg.Overrides != nil {
		t.Errorf("no [overrides] table: got %+v", cfg.Overrides)
	}
}

// repo-config, scenario Machine config sets the allow-list: tuckerbox
// check fails and says [overrides] belongs in repository config.
func TestOverridesMachineRefused(t *testing.T) {
	f := newFixture(t, minimal, "[overrides]\nagents = [\"codex\"]\n")
	wantProblem(t, f.problems(), f.machine, "overrides", "belongs in repository config")
	cfg, _ := f.load()
	if cfg.Overrides != nil {
		t.Errorf("machine [overrides] used: %+v", cfg.Overrides)
	}
}

// repo-config, scenario Allowed profile not defined on the machine: the
// list entry alone does not fail the config.
func TestOverridesProfileNotOnMachine(t *testing.T) {
	cfg := newFixture(t, minimal+"\n[overrides]\npermissions = [\"edit\"]\n", "").mustLoad()
	if !slices.Equal(cfg.Overrides.Permissions, []string{"edit"}) {
		t.Errorf("permissions: %v", cfg.Overrides.Permissions)
	}
	if msg := cfg.ProfileProblem("edit", "claude"); !strings.Contains(msg, `permission profile "edit" is not defined in machine config`) {
		t.Errorf("ProfileProblem: %q", msg)
	}
}

func TestOverridesValidation(t *testing.T) {
	f := newFixture(t, minimal+`
[overrides]
agents = ["claude", "claude", ""]
models = { "bad name" = ["x"] }
permissions = ["no/slash"]
colours = ["red"]
`, "")
	ps := f.problems()
	wantProblem(t, ps, f.repoPath(), "overrides.agents", `"claude" is listed twice`)
	wantProblem(t, ps, f.repoPath(), "overrides.agents", "empty value")
	wantProblem(t, ps, f.repoPath(), "overrides.models", `invalid agent name "bad name"`)
	wantProblem(t, ps, f.repoPath(), "overrides.permissions", `invalid name "no/slash"`)
	wantProblem(t, ps, f.repoPath(), "overrides.colours", "unknown key")
}

// The starter's commented example is a valid allow-list once uncommented.
func TestStarterOverridesExample(t *testing.T) {
	var lines []string
	in := false
	for _, l := range strings.Split(Starter, "\n") {
		switch {
		case l == "# [overrides]":
			in = true
		case in && !strings.HasPrefix(l, "# "):
			in = false
		}
		if in {
			l = strings.TrimPrefix(l, "# ")
		}
		lines = append(lines, l)
	}
	data := strings.Join(lines, "\n")
	if !strings.Contains(data, "\n[overrides]\n") {
		t.Fatal("starter has no [overrides] example")
	}
	cfg := newFixture(t, data, "").mustLoad()
	if cfg.Overrides == nil || !slices.Equal(cfg.Overrides.Models["codex"], []string{"gpt-5.5"}) {
		t.Errorf("overrides: %+v", cfg.Overrides)
	}
}

// RepoOverrides reads only the [overrides] table of a repository config
// copy (a worktree's or a commit's): other keys, even unknown ones, are not
// its business, but an undecodable file or an invalid table is refused.
func TestRepoOverrides(t *testing.T) {
	o, ps := RepoOverrides(".tuckerbox/config.toml", []byte("future_key = 1\n\n[overrides]\nagents = [\"codex\"]\nefforts = [\"high\"]\n\n[overrides.models]\ncodex = [\"gpt-5\"]\n"))
	if len(ps) != 0 || o == nil || !slices.Equal(o.Agents, []string{"codex"}) || !slices.Equal(o.Efforts, []string{"high"}) || !slices.Equal(o.Models["codex"], []string{"gpt-5"}) {
		t.Errorf("got %+v %v", o, ps)
	}
	if o, ps := RepoOverrides("c.toml", []byte("max_parallel = 2\n")); o != nil || len(ps) != 0 {
		t.Errorf("no table: got %+v %v", o, ps)
	}
	if o, ps := RepoOverrides("c.toml", []byte("[overrides]\nagents = [\"codex\", \"codex\"]\n")); o != nil || len(ps) == 0 || !strings.Contains(ps.Error(), "listed twice") {
		t.Errorf("invalid table: got %+v %v", o, ps)
	}
	if o, ps := RepoOverrides("c.toml", []byte("[overrides\n")); o != nil || len(ps) == 0 {
		t.Errorf("broken file: got %+v %v", o, ps)
	}
	if o, ps := RepoOverrides("c.toml", []byte("[overrides]\nagents = \"codex\"\n")); o != nil || len(ps) == 0 {
		t.Errorf("wrong type: got %+v %v", o, ps)
	}
}
