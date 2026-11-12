// Package deps reads the changes an OpenSpec change depends on and checks
// them. A change lists its dependencies in tuckerbox.toml inside its change
// folder:
//
//	depends_on = ["add-users"]
//
// The same file may hold other tables (such as [approval]); they are
// ignored here.
package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// FileName is the sidecar file in a change folder.
const FileName = "tuckerbox.toml"

// ChangesDir is the OpenSpec changes folder of the checkout at root.
func ChangesDir(root string) string { return filepath.Join(root, "openspec", "changes") }

// ArchiveDir is the OpenSpec archive folder of the checkout at root.
func ArchiveDir(root string) string { return filepath.Join(ChangesDir(root), "archive") }

// File is the part of tuckerbox.toml this package reads.
type File struct {
	DependsOn []string `toml:"depends_on"`
}

// Read returns the dependencies declared by change id in the checkout at
// root, in file order without repeats. A missing tuckerbox.toml, or one
// without depends_on, means none.
func Read(root, id string) ([]string, error) {
	p := filepath.Join(ChangesDir(root), id, FileName)
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	// Unknown keys and tables are allowed: toml.Decode only reports them
	// through MetaData.Undecoded, which is not checked.
	if _, err := toml.Decode(string(data), &f); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.ToSlash(rel(root, p)), err)
	}
	var out []string
	seen := map[string]bool{}
	for _, d := range f.DependsOn {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out, nil
}

// Active lists the ids of the active changes in the checkout at root: the
// folders in openspec/changes other than archive, sorted.
func Active(root string) ([]string, error) {
	entries, err := os.ReadDir(ChangesDir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "archive" && !strings.HasPrefix(e.Name(), ".") {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// IsActive reports whether id is an active change in the checkout at root.
func IsActive(root, id string) bool {
	if id == "archive" || statedir.CheckID("change", id) != nil {
		return false
	}
	st, err := os.Stat(filepath.Join(ChangesDir(root), id))
	return err == nil && st.IsDir()
}

// archived matches an OpenSpec archive folder name: YYYY-MM-DD-<id>.
var archived = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-(.+)$`)

// Archived lists the ids of the archived changes in the checkout at root.
func Archived(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(ArchiveDir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if m := archived.FindStringSubmatch(e.Name()); m != nil {
			out[m[1]] = true
		} else {
			out[e.Name()] = true
		}
	}
	return out, nil
}

// IsArchived reports whether id is an archived change in the checkout at
// root.
func IsArchived(root, id string) bool {
	a, err := Archived(root)
	return err == nil && a[id]
}

// Problem is one dependency problem found by Check.
type Problem struct {
	// File is the tuckerbox.toml the problem is in, relative to root and
	// with forward slashes; "" for a cycle.
	File string
	// Change is the change that declares the dependency.
	Change string
	// Msg says what is wrong.
	Msg string
}

func (p Problem) String() string {
	if p.File == "" {
		return p.Msg
	}
	return p.File + ": depends_on: " + p.Msg
}

// Check reads the dependencies of every active change in the checkout at
// root and returns each dependency that names neither an active nor an
// archived change, and each dependency cycle.
func Check(root string) ([]Problem, error) {
	active, err := Active(root)
	if err != nil {
		return nil, err
	}
	arch, err := Archived(root)
	if err != nil {
		return nil, err
	}
	isActive := map[string]bool{}
	for _, id := range active {
		isActive[id] = true
	}
	var problems []Problem
	graph := map[string][]string{}
	for _, id := range active {
		file := "openspec/changes/" + id + "/" + FileName
		ds, err := Read(root, id)
		if err != nil {
			problems = append(problems, Problem{File: file, Change: id, Msg: err.Error()})
			continue
		}
		for _, d := range ds {
			switch {
			case isActive[d]:
				graph[id] = append(graph[id], d)
			case arch[d]:
			default:
				problems = append(problems, Problem{File: file, Change: id,
					Msg: fmt.Sprintf("change %s depends on %s, which is neither an active nor an archived change", id, d)})
			}
		}
	}
	for _, c := range Cycles(graph) {
		problems = append(problems, Problem{Change: c[0],
			Msg: "dependency cycle: " + strings.Join(append(c, c[0]), " -> ")})
	}
	return problems, nil
}

// Cycles returns the cycles in graph (change -> the changes it depends
// on), each once, starting at its smallest id, sorted. A change that
// depends on itself is a cycle of one.
func Cycles(graph map[string][]string) [][]string {
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	var path []string
	seen := map[string]bool{}
	var out [][]string
	var visit func(n string)
	visit = func(n string) {
		colour[n] = grey
		path = append(path, n)
		for _, m := range graph[n] {
			switch colour[m] {
			case white:
				visit(m)
			case grey:
				// path from m to n is a cycle.
				i := len(path) - 1
				for path[i] != m {
					i--
				}
				c := rotate(append([]string(nil), path[i:]...))
				if k := strings.Join(c, "\x00"); !seen[k] {
					seen[k] = true
					out = append(out, c)
				}
			}
		}
		path = path[:len(path)-1]
		colour[n] = black
	}
	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if colour[n] == white {
			visit(n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], " ") < strings.Join(out[j], " ")
	})
	return out
}

// rotate turns a cycle so that it starts at its smallest id.
func rotate(c []string) []string {
	min := 0
	for i := range c {
		if c[i] < c[min] {
			min = i
		}
	}
	return append(c[min:], c[:min]...)
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}

// Ready is the run state a dependency must reach on this machine, unless
// it is archived.
const Ready = "ready"

// Lookup returns the state and current step of change id's run on this
// machine, or "" when it has no run.
type Lookup func(id string) (state, step string)

// Unmet is a dependency that holds a run back.
type Unmet struct {
	ID string
	// Why is the dependency's step while it runs, else its state, or
	// "not started".
	Why string
}

func (u Unmet) String() string { return u.ID + " (" + u.Why + ")" }

// Reason is the queued reason for a run held back by unmet.
func Reason(unmet []Unmet) string {
	parts := make([]string, len(unmet))
	for i, u := range unmet {
		parts[i] = u.String()
	}
	return "waiting on " + strings.Join(parts, ", ")
}

// Pending returns the dependencies in ids that are neither archived in any
// of roots nor ready according to lookup. Those that are not active in any
// root either are returned as unknown: no run can make them ready.
func Pending(roots []string, ids []string, lookup Lookup) (unmet []Unmet, unknown []string) {
	for _, d := range ids {
		if anyRoot(roots, d, IsArchived) {
			continue
		}
		state, step := lookup(d)
		switch {
		case state == Ready:
			continue
		case state == "" && !anyRoot(roots, d, IsActive):
			unknown = append(unknown, d)
			continue
		}
		why := state
		switch {
		case state == "":
			why = "not started"
		case state == "running" && step != "":
			why = step
		}
		unmet = append(unmet, Unmet{ID: d, Why: why})
	}
	return unmet, unknown
}

func anyRoot(roots []string, id string, f func(root, id string) bool) bool {
	for _, r := range roots {
		if r != "" && f(r, id) {
			return true
		}
	}
	return false
}
