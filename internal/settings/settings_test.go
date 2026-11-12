package settings

import (
	"slices"
	"strings"
	"testing"
)

func TestParseKeepsOtherKeys(t *testing.T) {
	data := `title = "Login page"
depends_on = ["add-users", " add-users "]
priority = 3000
machines = ["studio", "mini"]

[steps.build]
agent = "codex"
model = "gpt-5.5"
effort = "high"

[steps.review]
permissions = "readonly"

[approval]
hash = "sha256:00"
by = "Sam Example"
at = 2026-11-12T09:00:00+10:00
`
	sc, ps, err := Parse([]byte(data))
	if err != nil || len(ps) != 0 {
		t.Fatalf("Parse: %v %v", err, ps)
	}
	if sc.Title != "Login page" || !slices.Equal(sc.DependsOn, []string{"add-users"}) {
		t.Errorf("title or depends_on: %+v", sc)
	}
	if !slices.Equal(sc.Machines, []string{"studio", "mini"}) {
		t.Errorf("machines: %v", sc.Machines)
	}
	if got := sc.Steps["build"]; got != (Override{Agent: "codex", Model: "gpt-5.5", Effort: "high"}) {
		t.Errorf("build: %+v", got)
	}
	if got := sc.Steps["review"]; got != (Override{Permissions: "readonly"}) {
		t.Errorf("review: %+v", got)
	}
}

func TestParseEmptyFile(t *testing.T) {
	sc, ps, err := Parse(nil)
	if err != nil || len(ps) != 0 || len(sc.Steps) != 0 || sc.Machines != nil {
		t.Errorf("Parse(nil): %+v %v %v", sc, ps, err)
	}
}

// change-settings, scenario Raw flags in an override: refused by key,
// saying flags belong in machine config, without the value.
func TestParseRefusesOtherKeys(t *testing.T) {
	data := `[steps.build]
agent = "codex"
args = ["--dangerously-skip-permissions"]
command = ["sh", "-c", "secret-command"]
role = "planner"

[steps.build.sandbox]
mode = "danger-full-access"
`
	sc, ps, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Steps["build"].Agent != "codex" {
		t.Errorf("agent lost: %+v", sc.Steps["build"])
	}
	want := map[string]string{
		"steps.build.args":    "belong in a permission profile in machine config",
		"steps.build.sandbox": "belong in a permission profile in machine config",
		"steps.build.command": "may set only agent, model, effort, permissions",
		"steps.build.role":    "may set only agent, model, effort, permissions",
	}
	if len(ps) != len(want) {
		t.Errorf("problems: %v", ps)
	}
	for _, p := range ps {
		if frag, ok := want[p.Key]; !ok || !strings.Contains(p.Message, frag) {
			t.Errorf("unexpected problem %v", p)
		}
		for _, secret := range []string{"--dangerously-skip-permissions", "secret-command", "danger-full-access"} {
			if strings.Contains(p.String(), secret) {
				t.Errorf("problem prints the value %q: %v", secret, p)
			}
		}
	}
}

// Dotted keys make implicit tables the decoder does not list as undecoded,
// so each refused key in a step table is reported by its own name, once,
// however deep the value sits.
func TestParseRefusesDottedKeys(t *testing.T) {
	data := `[steps.build]
agent = "codex"
args.codex = ["--dangerously-skip-permissions"]
args.claude = ["--dangerously-skip-permissions"]
extra.deep.key = "secret-command"
steps_flags.x = "danger-full-access"

[steps.review.sandbox.inner]
mode = "danger-full-access"
`
	sc, ps, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if sc.Steps["build"].Agent != "codex" {
		t.Errorf("agent lost: %+v", sc.Steps["build"])
	}
	want := map[string]string{
		"steps.build.args":        "belong in a permission profile in machine config",
		"steps.build.extra":       "may set only agent, model, effort, permissions",
		"steps.build.steps_flags": "may set only agent, model, effort, permissions",
		"steps.review.sandbox":    "belong in a permission profile in machine config",
	}
	got := map[string]int{}
	for _, p := range ps {
		got[p.Key]++
		if frag, ok := want[p.Key]; !ok || !strings.Contains(p.Message, frag) {
			t.Errorf("unexpected problem %v", p)
		}
		for _, secret := range []string{"--dangerously-skip-permissions", "secret-command", "danger-full-access"} {
			if strings.Contains(p.String(), secret) {
				t.Errorf("problem prints the value %q: %v", secret, p)
			}
		}
	}
	for k := range want {
		if got[k] != 1 {
			t.Errorf("%s reported %d times, want once: %v", k, got[k], ps)
		}
	}
}

func TestParseEmptyValues(t *testing.T) {
	sc, ps, err := Parse([]byte("machines = [\"\"]\n[steps.build]\nmodel = \" \"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].Key != "machines" || ps[1].Key != "steps.build.model" {
		t.Errorf("problems: %v", ps)
	}
	if sc.Steps["build"].Model != "" || len(sc.Machines) != 0 {
		t.Errorf("empty values kept: %+v", sc)
	}
}

func TestParseWrongType(t *testing.T) {
	for _, data := range []string{"[steps.build]\nagent = 3\n", "machines = \"mini\"\n", "steps = 1\n"} {
		if _, _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%q: no error", data)
		}
	}
}
