package priority

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func ids(cs []Change) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func reqIDs(rs []Request) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

// writeChange makes openspec/changes/<id> with an optional tuckerbox.toml
// and .openspec.yaml.
func writeChange(t *testing.T, root, id, sidecar, created string) {
	t.Helper()
	dir := filepath.Join(root, "openspec", "changes", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if sidecar != "" {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(sidecar), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if created != "" {
		if err := os.WriteFile(filepath.Join(dir, ".openspec.yaml"), []byte("schema: spec-driven\ncreated: "+created+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Scenario: Ranked and unranked. A 2000, B 1000, C and D none, D older
// than C: B, A, D, C.
func TestRankedAndUnranked(t *testing.T) {
	root := t.TempDir()
	writeChange(t, root, "a", "priority = 2000\n", "2026-01-01")
	writeChange(t, root, "b", "title = \"B\"\npriority = 1000\n", "2026-01-05")
	writeChange(t, root, "c", "", "2026-03-01")
	writeChange(t, root, "d", "depends_on = []\n", "2026-02-01")
	cs, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(Sort(cs)); !reflect.DeepEqual(got, []string{"b", "a", "d", "c"}) {
		t.Errorf("order %v", got)
	}
	pos := Positions(cs)
	if pos["b"] != 1 || pos["c"] != 4 {
		t.Errorf("positions %v", pos)
	}
}

// Ties: same value, then age, then id; unknown age after known.
func TestTies(t *testing.T) {
	cs := []Change{
		{ID: "z", Priority: 5, Created: day("2026-01-01")},
		{ID: "y", Priority: 5, Created: day("2026-02-01")},
		{ID: "x", Priority: 5},
		{ID: "w", Priority: 5, Created: day("2026-01-01")},
	}
	if got := ids(Sort(cs)); !reflect.DeepEqual(got, []string{"w", "z", "y", "x"}) {
		t.Errorf("order %v", got)
	}
}

// Scenario: Invalid priority. A value that is not a positive integer is a
// problem naming the file, and counts as none.
func TestInvalidPriority(t *testing.T) {
	root := t.TempDir()
	writeChange(t, root, "add-login", "priority = \"high\"\n", "")
	writeChange(t, root, "neg", "priority = -3\n", "")
	writeChange(t, root, "flt", "priority = 1.5\n", "")
	writeChange(t, root, "ok", "priority = 7\n", "")
	writeChange(t, root, "broken", "priority = \n", "")
	ps, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ps {
		got = append(got, p.String())
	}
	want := []string{
		`openspec/changes/add-login/tuckerbox.toml: priority: must be a positive integer, not "high"`,
		`openspec/changes/flt/tuckerbox.toml: priority: must be a positive integer, not 1.5`,
		`openspec/changes/neg/tuckerbox.toml: priority: must be a positive integer, not -3`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("problems:\n%s", strings.Join(got, "\n"))
	}
	cs, _ := Load(root)
	if got := ids(Sort(cs)); got[0] != "ok" {
		t.Errorf("invalid values should count as none: %v", got)
	}
}

func abcd(vals ...int64) []Change {
	var cs []Change
	for i, id := range []string{"a", "b", "c", "d"}[:len(vals)] {
		cs = append(cs, Change{ID: id, Priority: vals[i], Created: day("2026-01-01").AddDate(0, 0, i)})
	}
	return cs
}

// apply returns cs with m's values written.
func apply(cs []Change, m Move) []Change {
	out := append([]Change(nil), cs...)
	for i := range out {
		if v, ok := m.Values[out[i].ID]; ok {
			out[i].Priority = v
		}
	}
	return out
}

// Scenario: Move to a position. A, B, C, D (all unranked): D to 2 gives
// A, D, B, C. The changes above get values, the ones below stay unranked.
func TestMoveToAPosition(t *testing.T) {
	cs := abcd(0, 0, 0, 0)
	m, err := Plan(cs, "d", Target{Position: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(Sort(apply(cs, m))); !reflect.DeepEqual(got, []string{"a", "d", "b", "c"}) {
		t.Errorf("order %v", got)
	}
	if !reflect.DeepEqual(m.Values, map[string]int64{"a": 1000, "d": 2000}) {
		t.Errorf("values %v", m.Values)
	}
	if m.From != 4 || m.To != 2 || !reflect.DeepEqual(m.Order, []string{"a", "d", "b", "c"}) {
		t.Errorf("move %+v", m)
	}
}

// Scenario: Up at the top: nothing changes. Down at the bottom too.
func TestUpAtTheTop(t *testing.T) {
	cs := abcd(1000, 0, 0)
	for _, c := range []struct {
		id string
		t  Target
	}{{"a", Target{Rel: "up"}}, {"a", Target{Rel: "top"}}, {"a", Target{Position: 1}}, {"c", Target{Rel: "down"}}, {"c", Target{Position: 99}}} {
		m, err := Plan(cs, c.id, c.t)
		if err != nil || len(m.Values) != 0 || m.From != m.To {
			t.Errorf("%s %+v: %+v %v", c.id, c.t, m, err)
		}
	}
	if _, err := Plan(cs, "nope", Target{Rel: "top"}); err != ErrUnknown {
		t.Errorf("unknown: %v", err)
	}
}

// Scenario: Gap available. A 1000, B 2000, C 3000; C to 2 changes C only.
func TestGapAvailable(t *testing.T) {
	cs := abcd(1000, 2000, 3000)
	m, err := Plan(cs, "c", Target{Position: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Values) != 1 || m.Values["c"] <= 1000 || m.Values["c"] >= 2000 || m.Renumbered {
		t.Errorf("values %v", m.Values)
	}
	if got := ids(Sort(apply(cs, m))); !reflect.DeepEqual(got, []string{"a", "c", "b"}) {
		t.Errorf("order %v", got)
	}
}

// Scenario: No gap left. A 1000, B 1001; C between them renumbers A, C, B.
func TestNoGapLeft(t *testing.T) {
	cs := abcd(1000, 1001, 0)
	m, err := Plan(cs, "c", Target{Position: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Renumbered || !reflect.DeepEqual(m.Values, map[string]int64{"c": 2000, "b": 3000}) {
		t.Errorf("move %+v", m)
	}
	if got := ids(Sort(apply(cs, m))); !reflect.DeepEqual(got, []string{"a", "c", "b"}) {
		t.Errorf("order %v", got)
	}
}

// Every move of every change to every position, over value layouts with
// ties, gaps and unranked changes, gives exactly the order asked for, and
// changes only unranked changes above the new position, besides values.
func TestPlanExhaustive(t *testing.T) {
	layouts := [][]int64{
		{0, 0, 0, 0, 0},
		{1000, 2000, 3000, 0, 0},
		{1, 2, 3, 4, 5},
		{5, 5, 5, 0, 0},
		{1, 1, 2, 2, 0},
		{1000, 1001, 1002, 1003, 1004},
		{1, 0, 0, 0, 0},
	}
	for _, vals := range layouts {
		var cs []Change
		for i, v := range vals {
			cs = append(cs, Change{ID: string(rune('a' + i)), Priority: v, Created: day("2026-01-01").AddDate(0, 0, i)})
		}
		before := ids(Sort(cs))
		for _, c := range cs {
			for to := 1; to <= len(cs)+1; to++ {
				m, err := Plan(cs, c.ID, Target{Position: to})
				if err != nil {
					t.Fatal(err)
				}
				got := ids(Sort(apply(cs, m)))
				if !reflect.DeepEqual(got, m.Order) {
					t.Errorf("%v move %s to %d: order %v, want %v (values %v)", vals, c.ID, to, got, m.Order, m.Values)
				}
				// The others keep their order.
				var rest, restBefore []string
				for _, id := range got {
					if id != c.ID {
						rest = append(rest, id)
					}
				}
				for _, id := range before {
					if id != c.ID {
						restBefore = append(restBefore, id)
					}
				}
				if !reflect.DeepEqual(rest, restBefore) {
					t.Errorf("%v move %s to %d: others reordered %v", vals, c.ID, to, got)
				}
				for id, v := range m.Values {
					if v <= 0 {
						t.Errorf("%v move %s to %d: %s gets %d", vals, c.ID, to, id, v)
					}
				}
				if !m.Renumbered && len(m.Values) > 0 {
					// Without a renumber, only the moved change and unranked
					// changes above it are written.
					for id := range m.Values {
						if id == c.ID {
							continue
						}
						for _, x := range cs {
							if x.ID == id && x.Ranked() {
								t.Errorf("%v move %s to %d: ranked %s rewritten without a renumber", vals, c.ID, to, id)
							}
						}
					}
				}
			}
		}
	}
}

func TestParseTarget(t *testing.T) {
	for _, s := range []string{"0", "-1", "x", "", "1.5"} {
		if _, err := ParseTarget(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	if tg, err := ParseTarget("3"); err != nil || tg.Position != 3 {
		t.Errorf("3: %+v %v", tg, err)
	}
	if tg, err := ParseTarget("up"); err != nil || tg.Rel != "up" {
		t.Errorf("up: %+v %v", tg, err)
	}
}

// Scenario: Dependency moves up. B 1000 depends on A (none), C 2000: A
// comes before C.
func TestDependencyMovesUp(t *testing.T) {
	all := []Change{{ID: "a"}, {ID: "b", Priority: 1000, DependsOn: []string{"a"}}, {ID: "c", Priority: 2000}}
	got := reqIDs(Admission([]Request{{"c", 1}, {"a", 2}}, all))
	if !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Errorf("order %v", got)
	}
	// With B in the set too, A still comes first and B after it.
	got = reqIDs(Admission([]Request{{"c", 1}, {"b", 2}, {"a", 3}}, all))
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("order %v", got)
	}
}

// Scenario: Unranked changes keep request order: age does not count.
func TestUnrankedKeepRequestOrder(t *testing.T) {
	all := []Change{{ID: "b", Created: day("2026-02-01")}, {ID: "c", Created: day("2026-01-01")}}
	got := reqIDs(Admission([]Request{{"b", 10}, {"c", 20}}, all))
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("order %v", got)
	}
}

// Scenario: Branching dependencies. D 1000 depends on A and C, A depends
// on B; requested A, C, B, D: C, B, A, D.
func TestBranchingDependencies(t *testing.T) {
	all := []Change{
		{ID: "a", DependsOn: []string{"b"}},
		{ID: "b"},
		{ID: "c"},
		{ID: "d", Priority: 1000, DependsOn: []string{"a", "c"}},
	}
	got := reqIDs(Admission([]Request{{"a", 1}, {"c", 2}, {"b", 3}, {"d", 4}}, all))
	if !reflect.DeepEqual(got, []string{"c", "b", "a", "d"}) {
		t.Errorf("order %v", got)
	}
}

// A dependency reached through a change outside the set still orders, and
// a cycle does not stall the order.
func TestAdmissionIndirectAndCycle(t *testing.T) {
	all := []Change{
		{ID: "x", Priority: 1, DependsOn: []string{"mid"}},
		{ID: "mid", DependsOn: []string{"y"}},
		{ID: "y"},
	}
	if got := reqIDs(Admission([]Request{{"x", 1}, {"y", 2}}, all)); !reflect.DeepEqual(got, []string{"y", "x"}) {
		t.Errorf("indirect %v", got)
	}
	cyc := []Change{{ID: "p", DependsOn: []string{"q"}}, {ID: "q", DependsOn: []string{"p"}}, {ID: "r", Priority: 5}}
	if got := reqIDs(Admission([]Request{{"p", 1}, {"q", 2}, {"r", 3}, {"unknown", 0}}, cyc)); !reflect.DeepEqual(got, []string{"r", "unknown", "p", "q"}) {
		t.Errorf("cycle %v", got)
	}
}

func TestSetValue(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty", "", "priority = 1000\n"},
		{"keys only", "title = \"T\"\ndepends_on = [\"x\"]\n", "title = \"T\"\ndepends_on = [\"x\"]\npriority = 1000\n"},
		{"no final newline", "title = \"T\"", "title = \"T\"\npriority = 1000\n"},
		{"before the first table", "title = \"T\"\n\n# who approved\n[approval]\nhash = \"h\"\n",
			"title = \"T\"\n\n# who approved\npriority = 1000\n\n[approval]\nhash = \"h\"\n"},
		{"table first", "[approval]\nhash = \"h\"\n", "priority = 1000\n\n[approval]\nhash = \"h\"\n"},
		{"keys, blank, table", "title = \"T\"\n\n[approval]\nhash = \"h\"\n", "title = \"T\"\npriority = 1000\n\n[approval]\nhash = \"h\"\n"},
		{"replace keeps comment", "# top\npriority = 5 # mine\ntitle = \"T\"\n[steps.build]\npriority = 3\n",
			"# top\npriority = 1000 # mine\ntitle = \"T\"\n[steps.build]\npriority = 3\n"},
		{"crlf", "title = \"T\"\r\n\r\n[approval]\r\nhash = \"h\"\r\n", "title = \"T\"\r\npriority = 1000\r\n\r\n[approval]\r\nhash = \"h\"\r\n"},
		{"invalid value replaced", "priority = \"high\"\n", "priority = 1000\n"},
	}
	for _, c := range cases {
		got, err := SetValue([]byte(c.in), 1000)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s:\n%q\nwant\n%q", c.name, got, c.want)
		}
	}
	if _, err := SetValue([]byte("title = \n"), 1); err == nil {
		t.Error("invalid TOML rewritten")
	}
}
