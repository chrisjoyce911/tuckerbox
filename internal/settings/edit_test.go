package settings

import (
	"errors"
	"strings"
	"testing"
)

func mustKey(t *testing.T, s string) Key {
	t.Helper()
	k, err := ParseKey(s)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestParseKey(t *testing.T) {
	for in, want := range map[string]Key{
		"step.build.agent":        {Step: "build", Field: "agent"},
		"steps.review.effort":     {Step: "review", Field: "effort"},
		"step.fix_ci.permissions": {Step: "fix_ci", Field: "permissions"},
		"machines":                {Field: "machines"},
	} {
		if got, err := ParseKey(in); err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"step.build.args", "step.build", "agent", "step..agent", "step.a.b.agent", "title", "depends_on"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

const sample = `# Sidecar for add-login
depends_on = ["add-auth"] # first

[steps.build]
# Build with Codex for now
model = "gpt-5.5" # cheap

[approval]
hash = "sha256:abc"
by = "Someone"
at = 2026-11-01T09:00:00+10:00
`

// change-settings, scenario Set an agent (line level): the key is added to
// its step table; comments, depends_on and [approval] are kept as they
// were.
func TestSetValueKeepsOtherLines(t *testing.T) {
	out, changed, err := SetValue([]byte(sample), mustKey(t, "step.build.agent"), "codex")
	if err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	want := strings.Replace(sample, "model = \"gpt-5.5\" # cheap\n", "model = \"gpt-5.5\" # cheap\nagent = \"codex\"\n", 1)
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}

	// Rewritten in place, keeping its comment.
	out, changed, err = SetValue([]byte(sample), mustKey(t, "step.build.model"), "gpt-5.5-mini")
	if err != nil || !changed || string(out) != strings.Replace(sample, `"gpt-5.5" # cheap`, `"gpt-5.5-mini" # cheap`, 1) {
		t.Errorf("in place: %v %v\n%s", changed, err, out)
	}

	// Already so: unchanged.
	out, changed, err = SetValue([]byte(sample), mustKey(t, "step.build.model"), "gpt-5.5")
	if err != nil || changed || string(out) != sample {
		t.Errorf("same value: %v %v", changed, err)
	}

	// A new step table goes at the end.
	out, _, err = SetValue([]byte(sample), mustKey(t, "step.review.effort"), "high")
	if err != nil || string(out) != sample+"\n[steps.review]\neffort = \"high\"\n" {
		t.Errorf("new table: %v\n%s", err, out)
	}

	// machines goes before the first table, a blank line before it.
	out, _, err = SetValue([]byte(sample), mustKey(t, "machines"), "studio, mini")
	want = strings.Replace(sample, "# first\n", "# first\nmachines = [\"studio\", \"mini\"]\n", 1)
	if err != nil || string(out) != want {
		t.Errorf("machines: %v\n%s", err, out)
	}
	out, _, err = SetValue(out, mustKey(t, "machines"), "mini")
	if err != nil || !strings.Contains(string(out), "machines = [\"mini\"]\n\n[steps.build]") {
		t.Errorf("machines again: %v\n%s", err, out)
	}
	out, _, err = SetValue([]byte("[approval]\nhash = \"x\"\n"), mustKey(t, "machines"), "mini")
	if err != nil || string(out) != "machines = [\"mini\"]\n\n[approval]\nhash = \"x\"\n" {
		t.Errorf("machines before only a table: %v\n%q", err, out)
	}

	// An empty file, and CRLF line endings.
	out, _, err = SetValue(nil, mustKey(t, "step.build.agent"), "codex")
	if err != nil || string(out) != "[steps.build]\nagent = \"codex\"\n" {
		t.Errorf("empty: %v %q", err, out)
	}
	crlf := "title = \"x\"\r\n\r\n[steps.build]\r\nmodel = \"a\"\r\n"
	out, _, err = SetValue([]byte(crlf), mustKey(t, "step.build.agent"), "codex")
	if err != nil || string(out) != crlf+"agent = \"codex\"\r\n" {
		t.Errorf("crlf: %v %q", err, out)
	}
}

// Set an agent (comments kept): a comment holding quotes or apostrophes,
// or a value holding a #, keeps the comment when the value is rewritten.
func TestSetValueKeepsCommentWithQuotes(t *testing.T) {
	for _, tc := range []struct{ old, want string }{
		{`model = "opus" # use "cheap" temporarily`, `model = "sonnet" # use "cheap" temporarily`},
		{`model = "opus" # don't keep this`, `model = "sonnet" # don't keep this`},
		{`model = "opus" # it's "fine"; ['x']`, `model = "sonnet" # it's "fine"; ['x']`},
		{`model = 'opus' # 'single' and "double"`, `model = "sonnet" # 'single' and "double"`},
		{`model = "a#b" # after`, `model = "sonnet" # after`},
		{`model = "a\"#b" # after "q"`, `model = "sonnet" # after "q"`},
		{`model = 'a\'  # it's`, `model = "sonnet" # it's`},
		{`model = """a"#""" # x "y"`, `model = "sonnet" # x "y"`},
		{`model = "opus"`, `model = "sonnet"`},
		{`model = "opus" #`, `model = "sonnet" #`},
	} {
		data := "[steps.build]\n" + tc.old + "\n"
		out, changed, err := SetValue([]byte(data), mustKey(t, "step.build.model"), "sonnet")
		if err != nil || !changed || string(out) != "[steps.build]\n"+tc.want+"\n" {
			t.Errorf("%s: %v %v\n%s", tc.old, changed, err, out)
		}
	}
}

func TestCommentStart(t *testing.T) {
	for v, want := range map[string]int{
		` "x"`:              -1,
		` "x" # c`:          5,
		` "#" # c`:          5,
		` '#\' # c`:         6,
		` "\\" # "c"`:       6,
		` ["a#", 'b#'] # c`: 14,
		` "unterminated #`:  -1,
	} {
		if got := commentStart(v); got != want {
			t.Errorf("%q: got %d, want %d", v, got, want)
		}
	}
}

func TestSetValueRefuses(t *testing.T) {
	for _, tc := range []struct{ data, key, value, want string }{
		{"", "step.build.agent", "", "empty value"},
		{"", "step.build.agent", " ", "empty value"},
		{"", "machines", "a,,b", "entry 2 is empty"},
		{"", "step.build.agent", "co\x01dex", "control character"},
		{"not toml = ", "step.build.agent", "codex", "not valid TOML"},
	} {
		if _, _, err := SetValue([]byte(tc.data), mustKey(t, tc.key), tc.value); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q %s=%q: %v", tc.data, tc.key, tc.value, err)
		}
	}
	// Layouts the line editor does not handle are refused, not mangled.
	for _, data := range []string{
		"steps.build.model = \"a\"\n",
		"[steps]\nbuild = { model = \"a\" }\n",
		"machines = [\n  \"a\",\n]\n",
	} {
		k := mustKey(t, "step.build.agent")
		if strings.HasPrefix(data, "machines") {
			k = mustKey(t, "machines")
		}
		if _, _, err := SetValue([]byte(data), k, "codex"); !errors.Is(err, ErrRewrite) {
			t.Errorf("%q: %v", data, err)
		}
	}
}

// change-settings, scenario Back to the repository default (line level):
// removing a step's only key removes its table too; removing a key that is
// not set changes nothing.
func TestUnsetValue(t *testing.T) {
	data := "depends_on = [\"add-auth\"]\n\n[steps.build]\nmodel = \"sonnet\"\n\n[approval]\nhash = \"x\"\n"
	out, changed, err := UnsetValue([]byte(data), mustKey(t, "step.build.model"))
	if err != nil || !changed || string(out) != "depends_on = [\"add-auth\"]\n\n[approval]\nhash = \"x\"\n" {
		t.Errorf("only key: %v %v\n%q", changed, err, out)
	}
	// The last table in the file.
	data = "title = \"x\"\n\n[steps.build]\nmodel = \"sonnet\"\n"
	out, _, err = UnsetValue([]byte(data), mustKey(t, "step.build.model"))
	if err != nil || string(out) != "title = \"x\"\n" {
		t.Errorf("last table: %v %q", err, out)
	}
	// Another key stays, with the table and its comment.
	data = "[steps.build]\n# why\nagent = \"codex\"\nmodel = \"sonnet\" # cheap\n"
	out, _, err = UnsetValue([]byte(data), mustKey(t, "step.build.model"))
	if err != nil || string(out) != "[steps.build]\n# why\nagent = \"codex\"\n" {
		t.Errorf("other key: %v %q", err, out)
	}
	// Not set: unchanged.
	for _, k := range []string{"step.build.effort", "step.review.model", "machines"} {
		out, changed, err = UnsetValue([]byte(data), mustKey(t, k))
		if err != nil || changed || string(out) != data {
			t.Errorf("%s not set: %v %v", k, changed, err)
		}
	}
	out, changed, err = UnsetValue([]byte("machines = [\"mini\"]\n\n[steps.build]\nagent = \"codex\"\n"), mustKey(t, "machines"))
	if err != nil || !changed || string(out) != "[steps.build]\nagent = \"codex\"\n" {
		t.Errorf("machines: %v %v %q", changed, err, out)
	}
	// Written as a dotted key: refused, not left half-removed.
	if _, _, err := UnsetValue([]byte("steps.build.model = \"a\"\n"), mustKey(t, "step.build.model")); !errors.Is(err, ErrRewrite) {
		t.Errorf("dotted: %v", err)
	}
}
