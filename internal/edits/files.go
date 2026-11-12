package edits

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// FileKinds are the files tuckerbox edit opens, besides spec:<capability>.
var FileKinds = []string{"proposal", "design", "model", "tasks"}

// File is one of a change's files, named as on the command line:
// proposal, design, model, tasks or spec:<capability>.
type File struct {
	// Kind is one of FileKinds, or "spec".
	Kind string
	// Capability is the spec delta's capability, for Kind "spec".
	Capability string
}

// ParseFile parses a file name as tuckerbox edit takes it.
func ParseFile(s string) (File, error) {
	for _, k := range FileKinds {
		if s == k {
			return File{Kind: k}, nil
		}
	}
	if c, ok := strings.CutPrefix(s, "spec:"); ok {
		if err := statedir.CheckID("capability", c); err != nil {
			return File{}, err
		}
		return File{Kind: "spec", Capability: c}, nil
	}
	return File{}, fmt.Errorf("unknown file %q: use %s or spec:<capability>", s, strings.Join(FileKinds, ", "))
}

func (f File) String() string {
	if f.Kind == "spec" {
		return "spec:" + f.Capability
	}
	return f.Kind
}

// Rel is the file's path in the change folder, with forward slashes.
func (f File) Rel() string {
	if f.Kind == "spec" {
		return "specs/" + f.Capability + "/spec.md"
	}
	return f.Kind + ".md"
}

// Path returns the file's path for p. A spec delta must exist; the other
// files may be missing, and the editor creates them.
func (f File) Path(p Place) (string, error) {
	path := filepath.Join(p.ChangeDir(), filepath.FromSlash(f.Rel()))
	if f.Kind != "spec" {
		return path, nil
	}
	if _, err := os.Stat(path); err != nil {
		deltas := SpecDeltas(p)
		list := "it has none"
		if len(deltas) > 0 {
			list = "its spec deltas: " + strings.Join(deltas, ", ")
		}
		return "", fmt.Errorf("change %s has no %s spec delta (%s); %s", p.Change, f.Capability, f.Rel(), list)
	}
	return path, nil
}

// SpecDeltas lists the capabilities the change has a spec delta for
// (specs/<capability>/spec.md), sorted.
func SpecDeltas(p Place) []string {
	entries, _ := os.ReadDir(filepath.Join(p.ChangeDir(), "specs"))
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(p.ChangeDir(), "specs", e.Name(), "spec.md")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}
