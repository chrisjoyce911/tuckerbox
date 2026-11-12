// Package approval approves OpenSpec changes before agents may start them.
//
// An approval is a hash of a change's text, kept in the [approval] table of
// the change's committed tuckerbox.toml with who approved and when. A
// change's approval state is a pure function of its files: approved when the
// stored hash matches them, stale when it no longer does, none without an
// approval. So approval lapses by itself, on every machine that has the same
// files, with no command needed.
//
// The hash (see [Hash]) covers proposal.md, design.md, model.md, tasks.md and
// every specs/**/spec.md, with line endings read as LF and task checkboxes
// read as unchecked, so ticking a task keeps an approval. tuckerbox.toml is
// not hashed: it holds the hash. A second hash, the settings hash (see
// [SettingsHash]), covers the change's own agent and permissions overrides
// in tuckerbox.toml; an approval is stale when either no longer matches.
package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Files is a change folder's files, wherever they are: on disk (a checkout
// or worktree) or in a git revision. Paths are slash-separated and relative
// to the change folder.
type Files interface {
	// Read returns a file's contents, or an error wrapping fs.ErrNotExist
	// when the file does not exist.
	Read(rel string) ([]byte, error)
	// Specs lists the spec delta files, specs/<capability>/spec.md (at
	// any depth below specs/).
	Specs() ([]string, error)
}

// hashVersion starts the hashed text, so a later change of the scheme can
// never match an old approval by accident.
const hashVersion = "tuckerbox-approval-v1\n"

// Prefix starts every approval hash.
const Prefix = "sha256:"

// fixedFiles are hashed whether or not they exist.
var fixedFiles = []string{"proposal.md", "design.md", "model.md", "tasks.md"}

// checkedBox matches a ticked task checkbox at the start of a line.
var checkedBox = regexp.MustCompile(`(?m)^([ \t]*- )\[[xX]\]`)

// Hash returns the approval hash of a change's files: "sha256:" and the hex
// SHA-256 of, for each file in path order, its path, then "missing" or its
// length and normalised content.
func Hash(files Files) (string, error) {
	specs, err := files.Specs()
	if err != nil {
		return "", err
	}
	names := append([]string{}, fixedFiles...)
	for _, s := range specs {
		if IsSpec(s) {
			names = append(names, s)
		}
	}
	sort.Strings(names)
	h := sha256.New()
	h.Write([]byte(hashVersion))
	for i, name := range names {
		if i > 0 && names[i-1] == name {
			continue
		}
		data, err := files.Read(name)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			fmt.Fprintf(h, "%s\nmissing\n", name)
			continue
		case err != nil:
			return "", err
		}
		data = Normalize(name, data)
		fmt.Fprintf(h, "%s\n%d\n", name, len(data))
		h.Write(data)
	}
	return Prefix + hex.EncodeToString(h.Sum(nil)), nil
}

// IsSpec reports whether rel (slash-separated, relative to the change
// folder) is a spec delta: a spec.md somewhere below specs/.
func IsSpec(rel string) bool {
	return strings.HasPrefix(rel, "specs/") && path.Base(rel) == "spec.md" && path.Clean(rel) == rel
}

// Normalize returns data as it is hashed: CRLF line endings as LF and, in
// tasks.md, every "- [x]" or "- [X]" checkbox as "- [ ]".
func Normalize(name string, data []byte) []byte {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if name == "tasks.md" {
		data = checkedBox.ReplaceAll(data, []byte("${1}[ ]"))
	}
	return data
}
