package settings

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// KeyMachines is the sidecar's top-level list of preferred machines, as a
// key for [ParseKey].
const KeyMachines = "machines"

// Key is one setting tuckerbox set can change: a step override
// (step.<step>.<field>) or the change's machines.
type Key struct {
	// Step is the step id; "" for machines.
	Step string
	// Field is one of [StepKeys], or [KeyMachines].
	Field string
}

// String is the key as tuckerbox set takes it: "step.build.agent" or
// "machines".
func (k Key) String() string {
	if k.Step == "" {
		return k.Field
	}
	return "step." + k.Step + "." + k.Field
}

// SidecarKey is the key as written in the sidecar and in problems:
// "steps.build.agent" or "machines".
func (k Key) SidecarKey() string {
	if k.Step == "" {
		return k.Field
	}
	return "steps." + k.Step + "." + k.Field
}

// bareKey is a TOML bare key: what a step id must be so it can be written
// as [steps.<id>] without quoting.
var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParseKey reads a key tuckerbox set takes: step.<step>.agent, model,
// effort or permissions, or machines. "steps." is accepted for "step.", as
// tuckerbox check names keys that way.
func ParseKey(s string) (Key, error) {
	if s == KeyMachines {
		return Key{Field: KeyMachines}, nil
	}
	rest, ok := strings.CutPrefix(s, "step.")
	if !ok {
		rest, ok = strings.CutPrefix(s, "steps.")
	}
	if ok {
		if i := strings.LastIndexByte(rest, '.'); i > 0 {
			step, field := rest[:i], rest[i+1:]
			if slices.Contains(StepKeys, field) {
				if !bareKey.MatchString(step) {
					return Key{}, fmt.Errorf("step %q: use letters, digits, '_' and '-' only", step)
				}
				return Key{Step: step, Field: field}, nil
			}
		}
	}
	return Key{}, fmt.Errorf("unknown key %q; use step.<step>.agent, step.<step>.model, step.<step>.effort, step.<step>.permissions or machines", s)
}

// ParseMachines reads machines as tuckerbox set takes it: names separated
// by commas, each trimmed and non-empty.
func ParseMachines(v string) ([]string, error) {
	var out []string
	for i, m := range strings.Split(v, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			return nil, fmt.Errorf("machines: entry %d is empty; list machine names separated by commas, or use --unset machines", i+1)
		}
		if err := plainValue(m); err != nil {
			return nil, fmt.Errorf("machines: %v", err)
		}
		out = append(out, m)
	}
	return out, nil
}

func plainValue(v string) error {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%q has a control character", v)
		}
	}
	return nil
}

// ErrRewrite means the sidecar is laid out in a way the line editor does
// not handle (such as an override written as a dotted key or an inline
// table), so it was not changed.
var ErrRewrite = errors.New("cannot rewrite " + FileName + " line by line; edit it by hand")

// SetValue returns a sidecar's contents with k set to v (for machines, the
// list v holds separated by commas), and whether anything changed. Only the
// key's own line is written: an existing line is rewritten in place,
// keeping a trailing comment; otherwise the line is added to the step's
// table, or a new table at the end of the file (machines: before the first
// table). Every other line, comments included, is kept. data may be empty.
//
// The result is decoded and compared with the original: if anything but k
// differs, it returns an error wrapping [ErrRewrite] instead.
func SetValue(data []byte, k Key, v string) ([]byte, bool, error) {
	before, err := decode(data)
	if err != nil {
		return nil, false, err
	}
	var line string
	var want any
	if k.Step == "" {
		ms, err := ParseMachines(v)
		if err != nil {
			return nil, false, err
		}
		quoted := make([]string, len(ms))
		wantList := make([]any, len(ms))
		for i, m := range ms {
			quoted[i] = quote(m)
			wantList[i] = m
		}
		line = k.Field + " = [" + strings.Join(quoted, ", ") + "]"
		want = wantList
	} else {
		if strings.TrimSpace(v) == "" {
			return nil, false, fmt.Errorf("%s: empty value; use --unset %s for the repository's value", k, k)
		}
		if err := plainValue(v); err != nil {
			return nil, false, fmt.Errorf("%s: %v", k, err)
		}
		line = k.Field + " = " + quote(v)
		want = v
	}
	if reflect.DeepEqual(lookup(before, k), want) {
		return data, false, nil
	}

	eol := lineEnding(data)
	lines := splitLines(data)
	var out []byte
	if k.Step == "" {
		out = setTopLevel(lines, k.Field, line, eol)
	} else {
		steps, _ := before["steps"].(map[string]any)
		if _, defined := steps[k.Step]; defined {
			if h, _ := findTable(lines, "steps."+k.Step); h < 0 {
				// Defined some other way (dotted keys, an inline table):
				// a new [steps.<step>] header would define it twice.
				return nil, false, ErrRewrite
			}
		}
		out = setInTable(lines, "steps."+k.Step, k.Field, line, eol)
	}

	expect := cloneMap(before)
	store(expect, k, want)
	if err := checkSame(out, expect); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// UnsetValue returns a sidecar's contents without k, and whether it had
// it. A [steps.<step>] table left with no keys is removed too. Every other
// line is kept.
func UnsetValue(data []byte, k Key) ([]byte, bool, error) {
	before, err := decode(data)
	if err != nil {
		return nil, false, err
	}
	if lookup(before, k) == nil {
		return data, false, nil
	}
	lines := splitLines(data)
	var out []byte
	if k.Step == "" {
		out = unsetTopLevel(lines, k.Field)
	} else {
		out = unsetInTable(lines, "steps."+k.Step, k.Field)
	}
	expect := cloneMap(before)
	store(expect, k, nil)
	if err := checkSame(out, expect); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func decode(data []byte) (map[string]any, error) {
	doc := map[string]any{}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid TOML, so it was not changed: %w", FileName, err)
	}
	return doc, nil
}

// lookup returns k's value in a decoded sidecar, or nil.
func lookup(doc map[string]any, k Key) any {
	if k.Step == "" {
		return doc[k.Field]
	}
	steps, _ := doc["steps"].(map[string]any)
	step, _ := steps[k.Step].(map[string]any)
	return step[k.Field]
}

// store sets k to v in a decoded sidecar, or removes it when v is nil,
// along with a step table and a steps table left empty.
func store(doc map[string]any, k Key, v any) {
	if k.Step == "" {
		if v == nil {
			delete(doc, k.Field)
		} else {
			doc[k.Field] = v
		}
		return
	}
	steps, _ := doc["steps"].(map[string]any)
	if steps == nil {
		steps = map[string]any{}
	}
	step, _ := steps[k.Step].(map[string]any)
	if step == nil {
		step = map[string]any{}
	}
	if v == nil {
		delete(step, k.Field)
	} else {
		step[k.Field] = v
	}
	steps[k.Step] = step
	if len(step) == 0 {
		delete(steps, k.Step)
	}
	doc["steps"] = steps
	if len(steps) == 0 {
		delete(doc, "steps")
	}
}

// cloneMap copies doc deeply enough for store: the steps table and its step
// tables.
func cloneMap(doc map[string]any) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	if steps, ok := doc["steps"].(map[string]any); ok {
		cs := make(map[string]any, len(steps))
		for id, s := range steps {
			if m, ok := s.(map[string]any); ok {
				cm := make(map[string]any, len(m))
				for k, v := range m {
					cm[k] = v
				}
				s = cm
			}
			cs[id] = s
		}
		out["steps"] = cs
	}
	return out
}

// checkSame makes sure a rewritten sidecar decodes to expect, so a layout
// the line editor does not understand never gets written.
func checkSame(out []byte, expect map[string]any) error {
	got := map[string]any{}
	if _, err := toml.Decode(string(out), &got); err != nil {
		return fmt.Errorf("%w (%v)", ErrRewrite, err)
	}
	// An explicit [steps] table left empty is the same as none.
	for _, m := range []map[string]any{got, expect} {
		if s, ok := m["steps"].(map[string]any); ok && len(s) == 0 {
			delete(m, "steps")
		}
	}
	if !reflect.DeepEqual(got, expect) {
		return ErrRewrite
	}
	return nil
}

// setInTable writes line as key in the table named table: in place of the
// key's line, else after the table's last key line (right after its header
// when it has none), else in a new table at the end of the file.
func setInTable(lines [][]byte, table, key, line, eol string) []byte {
	h, end := findTable(lines, table)
	if h < 0 {
		var out []byte
		for _, l := range lines {
			out = append(out, l...)
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, eol...)
		}
		if len(bytes.TrimSpace(out)) > 0 && !bytes.HasSuffix(out, []byte(eol+eol)) {
			out = append(out, eol...)
		}
		return append(out, "["+table+"]"+eol+line+eol...)
	}
	at := h + 1
	for i := h + 1; i < end; i++ {
		if isKeyLine(lines[i], key) {
			return replaceLine(lines, i, line, eol)
		}
		if t := bytes.TrimSpace(lines[i]); len(t) > 0 && t[0] != '#' {
			at = i + 1
		}
	}
	return insertLine(lines, at, line, eol)
}

// setTopLevel writes line as a top-level key: in place of the key's line,
// else after the last top-level line that is not blank, with a blank line
// before a table that would follow it directly.
func setTopLevel(lines [][]byte, key, line, eol string) []byte {
	first := len(lines)
	for i, l := range lines {
		if _, ok := header(l); ok {
			first = i
			break
		}
	}
	for i := 0; i < first; i++ {
		if isKeyLine(lines[i], key) {
			return replaceLine(lines, i, line, eol)
		}
	}
	at := first
	for at > 0 && len(bytes.TrimSpace(lines[at-1])) == 0 {
		at--
	}
	if at == first && first < len(lines) {
		// Directly before a header: separate them with a blank line.
		line += eol
	}
	return insertLine(lines, at, line, eol)
}

// unsetInTable removes key's line from the table named table, and the
// table's header and blank lines when no key is left in it. Comment lines
// are kept.
func unsetInTable(lines [][]byte, table, key string) []byte {
	h, end := findTable(lines, table)
	if h < 0 {
		return joinLines(lines)
	}
	keys := 0
	drop := -1
	for i := h + 1; i < end; i++ {
		if isKeyLine(lines[i], key) && drop < 0 {
			drop = i
			continue
		}
		if t := bytes.TrimSpace(lines[i]); len(t) > 0 && t[0] != '#' {
			keys++
		}
	}
	if drop < 0 {
		return joinLines(lines)
	}
	if keys > 0 {
		return joinLines(slices.Delete(slices.Clone(lines), drop, drop+1))
	}
	var out [][]byte
	out = append(out, lines[:h]...)
	for i := h + 1; i < end; i++ {
		if t := bytes.TrimSpace(lines[i]); len(t) > 0 && t[0] == '#' {
			out = append(out, lines[i])
		}
	}
	rest := lines[end:]
	// Drop the blank line that separated the table from what came before,
	// when it is now followed by another blank line or the end of the file.
	if len(out) > 0 && len(bytes.TrimSpace(out[len(out)-1])) == 0 &&
		(len(rest) == 0 || len(bytes.TrimSpace(rest[0])) == 0) {
		out = out[:len(out)-1]
	}
	return joinLines(append(out, rest...))
}

// unsetTopLevel removes a top-level key's line.
func unsetTopLevel(lines [][]byte, key string) []byte {
	for i, l := range lines {
		if _, ok := header(l); ok {
			break
		}
		if isKeyLine(l, key) {
			end := i + 1
			// A blank line now leading the file or doubling one before it
			// goes too.
			if end < len(lines) && len(bytes.TrimSpace(lines[end])) == 0 &&
				(i == 0 || len(bytes.TrimSpace(lines[i-1])) == 0) {
				end++
			}
			return joinLines(slices.Delete(slices.Clone(lines), i, end))
		}
	}
	return joinLines(lines)
}

// findTable returns the index of table's header line and of the next
// header line (len(lines) when it is the last table), or -1, -1.
func findTable(lines [][]byte, table string) (int, int) {
	h := -1
	for i, l := range lines {
		name, ok := header(l)
		if !ok {
			continue
		}
		if h >= 0 {
			return h, i
		}
		if name == table && !bytes.HasPrefix(bytes.TrimSpace(l), []byte("[[")) {
			h = i
		}
	}
	if h < 0 {
		return -1, -1
	}
	return h, len(lines)
}

// header returns the name of the table a line opens, such as "steps.build"
// for `[steps."build"]  # comment`.
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

// isKeyLine reports whether a line sets key: `key = …`, the key bare or
// quoted.
func isKeyLine(l []byte, key string) bool {
	t := strings.TrimSpace(string(l))
	for _, k := range []string{key, `"` + key + `"`, "'" + key + "'"} {
		if rest, ok := strings.CutPrefix(t, k); ok && strings.HasPrefix(strings.TrimSpace(rest), "=") {
			return true
		}
	}
	return false
}

// replaceLine returns lines with line i replaced by line, keeping its
// indentation, trailing comment and line ending.
func replaceLine(lines [][]byte, i int, line, eol string) []byte {
	old := lines[i]
	indent := old[:len(old)-len(bytes.TrimLeft(old, " \t"))]
	nl := string(indent) + line
	if c := trailingComment(old); c != "" {
		nl += " " + c
	}
	switch {
	case bytes.HasSuffix(old, []byte("\r\n")):
		nl += "\r\n"
	case bytes.HasSuffix(old, []byte("\n")):
		nl += "\n"
	}
	out := slices.Clone(lines)
	out[i] = []byte(nl)
	return joinLines(out)
}

// insertLine returns lines with line inserted before line at.
func insertLine(lines [][]byte, at int, line, eol string) []byte {
	var out []byte
	out = append(out, joinLines(lines[:at])...)
	if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, eol...)
	}
	out = append(out, line+eol...)
	return append(out, joinLines(lines[at:])...)
}

// trailingComment returns the "# …" after a one-line value, or "". It
// only looks after the last quote, so a # inside a string is not one.
func trailingComment(l []byte) string {
	s := strings.TrimRight(string(l), "\r\n")
	_, v, _ := strings.Cut(s, "=")
	from := strings.LastIndexAny(v, `"']`)
	i := strings.IndexByte(v[from+1:], '#')
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(v[from+1+i:])
}

func lineEnding(data []byte) string {
	if bytes.Contains(data, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
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

func joinLines(lines [][]byte) []byte {
	var out []byte
	for _, l := range lines {
		out = append(out, l...)
	}
	return out
}

// quote writes s as a TOML basic string. s has no control characters.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// CheckSet checks the sidecar a set would leave, after, against cfg's
// allow-list, and returns the problems it has with k's step that before
// did not have: a set never makes an override not allowed, but does not
// stop because of problems that were there already. Machine profile grants
// are not checked here; see [ProfileWarnings].
func CheckSet(cfg *config.Config, before, after Sidecar, k Key) []Problem {
	if k.Step == "" {
		return nil
	}
	only := func(sc Sidecar) Sidecar {
		o, ok := sc.Steps[k.Step]
		if !ok {
			return Sidecar{Steps: map[string]Override{}}
		}
		return Sidecar{Steps: map[string]Override{k.Step: o}}
	}
	old := map[string]bool{}
	for _, p := range check(cfg, only(before), false) {
		old[p.String()] = true
	}
	var out []Problem
	for _, p := range check(cfg, only(after), false) {
		if !old[p.String()] {
			out = append(out, p)
		}
	}
	return out
}

// ProfileWarnings returns why k's step could not use its permission
// profile on this machine, if it could not: machine config does not define
// it, or does not grant it here. A set still writes the value, because the
// change may run on another machine.
func ProfileWarnings(cfg *config.Config, sc Sidecar, k Key) []string {
	if k.Step == "" {
		return nil
	}
	o, ok := sc.Steps[k.Step]
	step, known := cfg.Step(k.Step)
	if !ok || !known {
		return nil
	}
	e := Resolve(cfg, step, o)
	if e.Permissions == "" || (k.Field != KeyPermissions && k.Field != KeyAgent) {
		return nil
	}
	if msg := cfg.ProfileProblem(e.Permissions, e.Agent); msg != "" {
		return []string{msg}
	}
	return nil
}
