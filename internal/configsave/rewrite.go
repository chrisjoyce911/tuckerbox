// Package configsave implements checked, serialised configuration saves.
package configsave

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

var ErrRewrite = errors.New("cannot rewrite this TOML layout line by line; use config edit")

// Edit sets a TOML value, or removes the key/table when Unset is true.
// Path contains decoded components (a forge host is one component).
type Edit struct {
	Path  []string
	Value any
	Unset bool
}

// ParsePath accepts TOML dotted paths, including quoted components.
func ParsePath(s string) ([]string, error) {
	var doc map[string]any
	md, err := toml.Decode(s+" = 0", &doc)
	if err != nil || len(md.Keys()) != 1 {
		return nil, fmt.Errorf("invalid config key")
	}
	return []string(md.Keys()[0]), nil
}
func PathString(p []string) string { return toml.Key(p).String() }

type statement struct {
	start, end int
	path       []string
	header     bool
}

func decode(data []byte) (map[string]any, error) {
	var m map[string]any
	_, err := toml.Decode(string(data), &m)
	if m == nil {
		m = map[string]any{}
	}
	return m, err
}

// layout scans complete TOML statements, using the decoder to find the end
// of arrays and multiline strings rather than treating their contents as keys.
func layout(lines []string) ([]statement, error) {
	var out []statement
	var table []string
	for i := 0; i < len(lines); i++ {
		s := strings.TrimSpace(lines[i])
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "[") {
			if strings.HasPrefix(s, "[[") {
				return nil, ErrRewrite
			}
			var m map[string]any
			md, err := toml.Decode(s+"\n__configsave_probe = 0", &m)
			if err != nil || len(md.Keys()) < 2 {
				return nil, ErrRewrite
			}
			table = slices.Clone([]string(md.Keys()[0]))
			out = append(out, statement{i, i + 1, table, true})
			continue
		}
		eq := assignment(s)
		if eq < 0 {
			return nil, ErrRewrite
		}
		key, err := ParsePath(strings.TrimSpace(s[:eq]))
		if err != nil {
			return nil, ErrRewrite
		}
		end := i + 1
		for ; end <= len(lines); end++ {
			if _, err := decode([]byte(strings.Join(lines[i:end], ""))); err == nil {
				break
			}
		}
		if end > len(lines) {
			return nil, ErrRewrite
		}
		out = append(out, statement{i, end, append(slices.Clone(table), key...), false})
		i = end - 1
	}
	return out, nil
}
func assignment(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == '\\' && quote == '"' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
		}
		if c == '=' {
			return i
		}
	}
	return -1
}
func prefix(a, b []string) bool { return len(a) >= len(b) && slices.Equal(a[:len(b)], b) }
func lookup(m map[string]any, p []string) any {
	var v any = m
	for _, k := range p {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[k]
	}
	return v
}
func store(m map[string]any, p []string, v any, unset bool) {
	for _, k := range p[:len(p)-1] {
		n, ok := m[k].(map[string]any)
		if !ok {
			n = map[string]any{}
			m[k] = n
		}
		m = n
	}
	if unset {
		delete(m, p[len(p)-1])
	} else {
		m[p[len(p)-1]] = v
	}
}
func prune(m map[string]any) {
	for k, v := range m {
		if n, ok := v.(map[string]any); ok {
			prune(n)
			if len(n) == 0 {
				delete(m, k)
			}
		}
	}
}
func encode(v any) (string, error) {
	var b bytes.Buffer
	err := toml.NewEncoder(&b).Encode(map[string]any{"value": v})
	if err != nil {
		return "", err
	}
	s := b.String()
	_, s, ok := strings.Cut(s, " = ")
	if !ok {
		return "", ErrRewrite
	}
	return strings.TrimSpace(s), nil
}
func comment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if q != 0 {
			if c == '\\' && q == '"' {
				i++
				continue
			}
			if c == q {
				q = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			q = c
		}
		if c == '#' {
			return strings.TrimRight(s[i:], "\r\n")
		}
	}
	return ""
}

// Rewrite preserves untouched bytes, including comments and line endings.
// It verifies the decoded result differs only by the requested edits.
func Rewrite(data []byte, edits []Edit) ([]byte, error) {
	var err error
	for _, e := range edits {
		data, err = rewriteOne(data, e)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}
func rewriteOne(data []byte, e Edit) ([]byte, error) {
	if len(e.Path) == 0 {
		return nil, ErrRewrite
	}
	before, err := decode(data)
	if err != nil {
		return nil, err
	}
	expected, _ := decode(data)
	if e.Unset && lookup(before, e.Path) == nil {
		return data, nil
	}
	var value string
	if !e.Unset {
		value, err = encode(e.Value)
		if err != nil {
			return nil, err
		}
		normalized, _ := decode([]byte("value = " + value))
		store(expected, e.Path, normalized["value"], false)
	} else {
		store(expected, e.Path, nil, true)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	stmts, err := layout(lines)
	if err != nil {
		return nil, err
	}
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	drop := map[int]bool{}
	found := false
	for _, s := range stmts {
		if (e.Unset && prefix(s.path, e.Path)) || (!s.header && slices.Equal(s.path, e.Path)) {
			if !e.Unset {
				if found {
					return nil, ErrRewrite
				}
				found = true
				indent := lines[s.start][:len(lines[s.start])-len(strings.TrimLeft(lines[s.start], " \t"))]
				suffix := ""
				if c := comment(lines[s.end-1]); c != "" {
					suffix = " " + c
				}
				ending := ""
				if strings.HasSuffix(lines[s.end-1], "\n") {
					ending = eol
				}
				lines[s.start] = indent + PathString(e.Path[len(e.Path)-1:]) + " = " + value + suffix + ending
				for i := s.start + 1; i < s.end; i++ {
					drop[i] = true
				}
			} else {
				for i := s.start; i < s.end; i++ {
					drop[i] = true
				}
			}
		}
	}
	if e.Unset {
		// Empty step and token-host tables disappear; surrounding comments stay.
		for _, s := range stmts {
			if s.header && len(s.path) == 2 && prefix(e.Path, s.path) && (s.path[0] == "steps" ||
				(s.path[0] == "forges" && len(e.Path) == 3 && e.Path[0] == "forges" && e.Path[2] == "token")) {
				n, _ := lookup(expected, s.path).(map[string]any)
				if len(n) == 0 {
					drop[s.start] = true
				}
			}
		}
	}
	var out string
	if !e.Unset && !found {
		parent := e.Path[:len(e.Path)-1]
		at := -1
		if len(parent) == 0 {
			at = 0
		}
		for _, s := range stmts {
			if s.header && slices.Equal(s.path, parent) {
				at = s.end
			}
			if !s.header && slices.Equal(s.path[:len(s.path)-1], parent) {
				at = s.end
			}
		}
		line := PathString(e.Path[len(e.Path)-1:]) + " = " + value + eol
		if at < 0 {
			out = strings.Join(lines, "")
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += eol
			}
			if out != "" {
				out += eol
			}
			out += "[" + PathString(parent) + "]" + eol + line
		} else {
			for i, l := range lines {
				if i == at {
					if out != "" && !strings.HasSuffix(out, "\n") {
						out += eol
					}
					out += line
				}
				out += l
			}
			if at == len(lines) {
				if out != "" && !strings.HasSuffix(out, "\n") {
					out += eol
				}
				out += line
			}
		}
	} else {
		for i, l := range lines {
			if !drop[i] {
				out += l
			}
		}
	}
	got, err := decode([]byte(out))
	if err != nil {
		return nil, ErrRewrite
	}
	prune(got)
	prune(expected)
	if !reflect.DeepEqual(got, expected) {
		return nil, ErrRewrite
	}
	return []byte(out), nil
}
