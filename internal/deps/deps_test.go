package deps

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// change makes an active change id in root; toml, if not "", is its
// tuckerbox.toml.
func change(t *testing.T, root, id, toml string) {
	t.Helper()
	dir := filepath.Join(ChangesDir(root), id)
	write(t, filepath.Join(dir, "proposal.md"), "# "+id+"\n")
	if toml != "" {
		write(t, filepath.Join(dir, FileName), toml)
	}
}

func archive(t *testing.T, root, id string) {
	t.Helper()
	write(t, filepath.Join(ArchiveDir(root), "2026-11-01-"+id, "proposal.md"), "# "+id+"\n")
}

func TestReadToleratesOtherTables(t *testing.T) {
	root := t.TempDir()
	change(t, root, "b", `# B needs the users table first.
depends_on = ["add-users", "a", "add-users"]
title = "B"

[approval]
hash = "sha256:00"
by = "Sam Example"
at = 2026-11-12T09:00:00+10:00
`)
	got, err := Read(root, "b")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"add-users", "a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadMissingFile(t *testing.T) {
	root := t.TempDir()
	change(t, root, "a", "")
	got, err := Read(root, "a")
	if err != nil || got != nil {
		t.Errorf("got %q, %v", got, err)
	}
	change(t, root, "c", "[approval]\nhash = \"x\"\n")
	if got, err := Read(root, "c"); err != nil || got != nil {
		t.Errorf("no depends_on: got %q, %v", got, err)
	}
}

func TestReadBadTOML(t *testing.T) {
	root := t.TempDir()
	change(t, root, "a", "depends_on = [\n")
	if _, err := Read(root, "a"); err == nil || !strings.Contains(err.Error(), "openspec/changes/a/tuckerbox.toml") {
		t.Errorf("err = %v", err)
	}
}

// Scenario: Unknown dependency. A change depends on add-login and no
// active or archived change has that id: check fails and names both.
func TestCheckUnknownDependency(t *testing.T) {
	root := t.TempDir()
	change(t, root, "b", `depends_on = ["add-login", "a", "old"]`)
	change(t, root, "a", "")
	archive(t, root, "old")
	ps, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("problems: %v", ps)
	}
	s := ps[0].String()
	for _, w := range []string{"openspec/changes/b/tuckerbox.toml", "change b", "add-login"} {
		if !strings.Contains(s, w) {
			t.Errorf("%q does not name %q", s, w)
		}
	}
}

// Scenario: Cycle. A depends on B and B depends on A: check fails and
// lists the cycle.
func TestCheckCycle(t *testing.T) {
	root := t.TempDir()
	change(t, root, "a", `depends_on = ["b"]`)
	change(t, root, "b", `depends_on = ["a"]`)
	change(t, root, "c", `depends_on = ["a"]`)
	ps, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].String() != "dependency cycle: a -> b -> a" {
		t.Errorf("problems: %v", ps)
	}
}

func TestCheckClean(t *testing.T) {
	root := t.TempDir()
	change(t, root, "a", "")
	change(t, root, "b", `depends_on = ["a", "old"]`)
	archive(t, root, "old")
	if ps, err := Check(root); err != nil || len(ps) != 0 {
		t.Errorf("%v %v", ps, err)
	}
	// No openspec folder at all: nothing to check.
	if ps, err := Check(t.TempDir()); err != nil || len(ps) != 0 {
		t.Errorf("%v %v", ps, err)
	}
}

func TestCycles(t *testing.T) {
	for _, tc := range []struct {
		graph map[string][]string
		want  [][]string
	}{
		{map[string][]string{"a": {"a"}}, [][]string{{"a"}}},
		{map[string][]string{"c": {"a"}, "a": {"b"}, "b": {"c"}}, [][]string{{"a", "b", "c"}}},
		{map[string][]string{"a": {"b"}, "b": {"c"}}, nil},
		{map[string][]string{"a": {"b"}, "b": {"a"}, "x": {"y"}, "y": {"x"}}, [][]string{{"a", "b"}, {"x", "y"}}},
	} {
		if got := Cycles(tc.graph); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Cycles(%v) = %v, want %v", tc.graph, got, tc.want)
		}
	}
}

func TestPending(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"a", "b", "c", "d"} {
		change(t, root, id, "")
	}
	archive(t, root, "old")
	other := t.TempDir()
	archive(t, other, "elsewhere")
	runs := map[string][2]string{
		"a": {"running", "build"},
		"b": {"ready", ""},
		"c": {"blocked", "review"},
	}
	lookup := func(id string) (string, string) { r := runs[id]; return r[0], r[1] }
	unmet, unknown := Pending([]string{root, other}, []string{"a", "b", "c", "d", "old", "elsewhere", "gone"}, lookup)
	if got := Reason(unmet); got != "waiting on a (build), c (blocked), d (not started)" {
		t.Errorf("reason %q", got)
	}
	if !reflect.DeepEqual(unknown, []string{"gone"}) {
		t.Errorf("unknown %q", unknown)
	}
}
