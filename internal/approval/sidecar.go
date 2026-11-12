package approval

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

// SidecarName is the Tuckerbox file inside a change folder. It also holds
// depends_on (change-dependencies) and is not part of the approval hash.
const SidecarName = "tuckerbox.toml"

// Approval is the [approval] table of a change's tuckerbox.toml.
type Approval struct {
	Hash string    `toml:"hash"`
	By   string    `toml:"by"`
	At   time.Time `toml:"at"`
}

// State is a change's approval state.
type State string

const (
	// Approved: the stored hash matches the change's files.
	Approved State = "approved"
	// Stale: the change has an approval that no longer matches its files.
	Stale State = "stale"
	// None: the change has no approval.
	None State = "none"
)

// Status is a change's approval state with what it was judged from.
type Status struct {
	State State
	// Approval is the stored approval; nil when State is None.
	Approval *Approval
	// Hash is the hash of the change's files now.
	Hash string
	// FromRef: judged from the approvals ref, not the sidecar.
	FromRef bool
}

// ParseSidecar returns the approval in a tuckerbox.toml's contents, or nil
// when it has none.
func ParseSidecar(data []byte) (*Approval, error) {
	var doc struct {
		Approval *Approval `toml:"approval"`
	}
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", SidecarName, err)
	}
	if !md.IsDefined("approval") {
		return nil, nil
	}
	if doc.Approval == nil || doc.Approval.Hash == "" {
		return nil, fmt.Errorf("%s: [approval] has no hash", SidecarName)
	}
	return doc.Approval, nil
}

// Judge returns the approval state of a change's files.
func Judge(files Files) (Status, error) {
	hash, err := Hash(files)
	if err != nil {
		return Status{}, err
	}
	st := Status{State: None, Hash: hash}
	data, err := files.Read(SidecarName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return st, nil
	case err != nil:
		return st, err
	}
	a, err := ParseSidecar(data)
	if err != nil || a == nil {
		return st, err
	}
	st.Approval = a
	st.State = Stale
	if a.Hash == hash {
		st.State = Approved
	}
	return st, nil
}

// Approve writes an [approval] table with the hash of d's files, by and at
// to d's tuckerbox.toml, keeping every other line of the file, and returns
// the approval written. It replaces an earlier approval.
func Approve(d Dir, by string, at time.Time) (Approval, error) {
	hash, err := Hash(d)
	if err != nil {
		return Approval{}, err
	}
	a := Approval{Hash: hash, By: by, At: at.Truncate(time.Second)}
	old, err := os.ReadFile(d.SidecarPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return a, err
	}
	data, err := SetApproval(old, a)
	if err != nil {
		return a, err
	}
	return a, WriteSidecar(d.SidecarPath(), data)
}

// Unapprove removes the [approval] table from d's tuckerbox.toml, keeping
// every other line, and reports whether there was one. A file left with
// nothing but blank lines is removed.
func Unapprove(d Dir) (bool, error) {
	old, err := os.ReadFile(d.SidecarPath())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data, removed, err := RemoveApproval(old)
	if err != nil || !removed {
		return false, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return true, os.Remove(d.SidecarPath())
	}
	return true, WriteSidecar(d.SidecarPath(), data)
}

// SetApproval returns a tuckerbox.toml's contents with its [approval]
// table set to a. An existing table is replaced where it is; otherwise the
// table is added at the end. Every other line is kept as it was.
func SetApproval(data []byte, a Approval) ([]byte, error) {
	a.At = a.At.Truncate(time.Second) // RFC 3339 as written has whole seconds
	if _, err := ParseSidecarLoose(data); err != nil {
		return nil, err
	}
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	table := strings.Join([]string{
		"[approval]",
		"hash = " + tomlString(a.Hash),
		"by = " + tomlString(a.By),
		"at = " + a.At.Format(time.RFC3339),
	}, eol) + eol
	lines := splitLines(data)
	start, end, found := findTable(lines)
	var out []byte
	if found {
		for _, l := range lines[:start] {
			out = append(out, l...)
		}
		out = append(out, table...)
		for _, l := range lines[end:] {
			out = append(out, l...)
		}
	} else {
		out = append(out, data...)
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, eol...)
		}
		if len(bytes.TrimSpace(out)) > 0 && !bytes.HasSuffix(out, []byte(eol+eol)) {
			out = append(out, eol...)
		}
		out = append(out, table...)
	}
	if err := checkRewrite(out, &a); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveApproval returns a tuckerbox.toml's contents without its
// [approval] table, and whether it had one. Every other line is kept.
func RemoveApproval(data []byte) ([]byte, bool, error) {
	has, err := ParseSidecarLoose(data)
	if err != nil {
		return nil, false, err
	}
	lines := splitLines(data)
	start, end, found := findTable(lines)
	if !found {
		if has {
			return nil, false, errors.New(SidecarName + ": approval is not written as an [approval] table; remove it by hand")
		}
		return data, false, nil
	}
	var out []byte
	for _, l := range lines[:start] {
		out = append(out, l...)
	}
	// Drop the blank line SetApproval put before the table.
	if end == len(lines) {
		for bytes.HasSuffix(out, []byte("\n\n")) || bytes.HasSuffix(out, []byte("\n\r\n")) {
			out = out[:len(out)-len(trailingEOL(out))]
		}
	}
	for _, l := range lines[end:] {
		out = append(out, l...)
	}
	if err := checkRewrite(out, nil); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// ParseSidecarLoose reports whether a tuckerbox.toml's contents define
// approval in any form, and fails only when the file is not valid TOML.
func ParseSidecarLoose(data []byte) (bool, error) {
	var doc map[string]any
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return false, fmt.Errorf("%s is not valid TOML, so it was not changed: %w", SidecarName, err)
	}
	return md.IsDefined("approval"), nil
}

// checkRewrite makes sure a rewritten file decodes, with the expected
// approval, so a layout the line editor does not understand (such as
// approval keys written as dotted keys elsewhere) never gets written.
func checkRewrite(data []byte, want *Approval) error {
	var doc map[string]any
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return fmt.Errorf("%s: cannot rewrite the [approval] table safely (%v); edit it by hand", SidecarName, err)
	}
	if want == nil {
		if md.IsDefined("approval") {
			return fmt.Errorf("%s: approval is also set outside the [approval] table; remove it by hand", SidecarName)
		}
		return nil
	}
	got, err := ParseSidecar(data)
	if err != nil || got == nil || got.Hash != want.Hash || got.By != want.By || !got.At.Equal(want.At) {
		return fmt.Errorf("%s: cannot rewrite the [approval] table safely; edit it by hand", SidecarName)
	}
	return nil
}

// splitLines splits data after each "\n", keeping the line endings.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			lines = append(lines, data)
			break
		}
		lines = append(lines, data[:i+1])
		data = data[i+1:]
	}
	return lines
}

func trailingEOL(b []byte) []byte {
	if bytes.HasSuffix(b, []byte("\r\n")) {
		return []byte("\r\n")
	}
	return []byte("\n")
}

// findTable finds the [approval] table's lines: from its header up to the
// next table header, leaving out the blank and comment lines just before
// that header (they belong with what follows).
func findTable(lines [][]byte) (start, end int, found bool) {
	start = -1
	for i, l := range lines {
		h, ok := header(l)
		if !ok {
			continue
		}
		if start >= 0 {
			end = i
			break
		}
		if h == "approval" {
			start = i
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	if end == 0 {
		end = len(lines)
	}
	for end > start+1 {
		t := bytes.TrimSpace(lines[end-1])
		if len(t) != 0 && t[0] != '#' {
			break
		}
		if end == len(lines) && len(t) == 0 {
			break // trailing blank lines at the end of the file: drop with the table
		}
		end--
	}
	return start, end, true
}

// header returns the name of the table a line opens, such as "approval"
// for "[approval]  # comment" or "x.y" for "[[x.y]]".
func header(line []byte) (string, bool) {
	t := strings.TrimSpace(string(line))
	if !strings.HasPrefix(t, "[") {
		return "", false
	}
	if i := strings.Index(t, "#"); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	t = strings.TrimSuffix(strings.TrimPrefix(t, "[["), "]]")
	t = strings.TrimSuffix(strings.TrimPrefix(t, "["), "]")
	parts := strings.Split(t, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, "."), true
}

// tomlString writes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f || r == utf8.RuneError:
			b.WriteString(`\u` + leftPad(strconv.FormatInt(int64(r), 16)))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func leftPad(h string) string { return strings.Repeat("0", 4-len(h)) + h }

// writeAtomic replaces path with data through a temporary file beside it
// (the approvals ref's local copy uses it too).
func writeAtomic(path string, data []byte) error { return WriteSidecar(path, data) }

// WriteSidecar replaces the tuckerbox.toml at path with data through a
// temporary file beside it and a rename, so readers see the old file or the
// new one, never part of it.
func WriteSidecar(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+SidecarName+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if fi, serr := os.Stat(path); serr == nil {
			_ = os.Chmod(tmp, fi.Mode().Perm())
		} else {
			_ = os.Chmod(tmp, 0o644)
		}
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
