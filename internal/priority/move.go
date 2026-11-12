package priority

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Target is where a move puts a change.
type Target struct {
	// Position is the position to move to, from 1; past the end means
	// last. Used when Rel is "".
	Position int
	// Rel is "up", "down" or "top", or "" for Position.
	Rel string
}

// ParseTarget reads n, "up", "down" or "top".
func ParseTarget(s string) (Target, error) {
	switch s {
	case "up", "down", "top":
		return Target{Rel: s}, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return Target{}, fmt.Errorf("%q is not a position (a number from 1), up, down or top", s)
	}
	return Target{Position: n}, nil
}

// Move is the outcome of [Plan].
type Move struct {
	// From and To are the change's positions before and after, from 1.
	From, To int
	// Values are the new priorities of the changes whose value changes,
	// the moved one included. Empty when nothing changes.
	Values map[string]int64
	// Renumbered is true when no integer gap was left, so the ranked
	// changes were given new values 1000, 2000, … in the new order.
	Renumbered bool
	// Order is the new priority order.
	Order []string
}

// ErrUnknown is a change that is not in the order.
var ErrUnknown = errors.New("not an active change")

// Plan moves change id within cs (any order; sorted here) to t, and
// returns the values to write. The other changes keep their order.
// Unranked changes above the new position are ranked, in their current
// order, with gaps of [Gap]. The moved change gets a value between its new
// neighbours; only when no integer gap is left are the ranked changes
// renumbered.
func Plan(cs []Change, id string, t Target) (Move, error) {
	order := Sort(cs)
	from := -1
	for i, c := range order {
		if c.ID == id {
			from = i
		}
	}
	if from < 0 {
		return Move{}, ErrUnknown
	}
	to := t.Position - 1
	switch t.Rel {
	case "top":
		to = 0
	case "up":
		to = from - 1
	case "down":
		to = from + 1
	}
	if to < 0 {
		to = 0
	}
	if to > len(order)-1 {
		to = len(order) - 1
	}
	m := Move{From: from + 1, To: to + 1, Values: map[string]int64{}}
	moved := order[from]
	rest := append(append([]Change(nil), order[:from]...), order[from+1:]...)
	next := append(append(append([]Change(nil), rest[:to]...), moved), rest[to:]...)
	for _, c := range next {
		m.Order = append(m.Order, c.ID)
	}
	if to == from {
		return m, nil
	}
	vals := make([]int64, len(next))
	for i, c := range next {
		vals[i] = c.Priority
	}
	// Rank the unranked changes above the new position, in their order.
	// They are the last ones above it, since unranked come after ranked;
	// so everything below the moved change is unranked too.
	var lo int64
	for i := 0; i < to; i++ {
		if vals[i] == 0 {
			if lo > math.MaxInt64-Gap {
				return renumber(m, next, to), nil
			}
			vals[i] = lo + Gap
		}
		lo = vals[i]
	}
	var v int64
	switch {
	case to+1 >= len(next) || vals[to+1] == 0:
		if lo > math.MaxInt64-Gap {
			return renumber(m, next, to), nil
		}
		v = lo + Gap
	case vals[to+1]-lo >= 2:
		v = lo + (vals[to+1]-lo)/2
	default:
		return renumber(m, next, to), nil
	}
	vals[to] = v
	for i, c := range next {
		if vals[i] != c.Priority {
			m.Values[c.ID] = vals[i]
		}
	}
	return m, nil
}

// renumber gives every change that is ranked in the new order, and those
// above the moved one, the values 1000, 2000, … in order.
func renumber(m Move, next []Change, to int) Move {
	m.Renumbered = true
	m.Values = map[string]int64{}
	k := int64(0)
	for i, c := range next {
		if i > to && !c.Ranked() {
			break
		}
		k++
		if v := k * Gap; v != c.Priority {
			m.Values[c.ID] = v
		}
	}
	return m
}

// SetValue returns a tuckerbox.toml's contents with its top-level priority
// set to v. An existing priority line is rewritten in place, keeping a
// trailing comment; otherwise the line is inserted before the first table,
// as TOML requires. Every other line is kept. data may be empty (no file
// yet).
func SetValue(data []byte, v int64) ([]byte, error) {
	var before map[string]any
	if _, err := toml.Decode(string(data), &before); err != nil {
		return nil, fmt.Errorf("%s is not valid TOML, so it was not changed: %w", FileName, err)
	}
	eol := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		eol = "\r\n"
	}
	line := "priority = " + strconv.FormatInt(v, 10)
	lines := splitLines(data)
	first := len(lines) // the first table header
	for i, l := range lines {
		if t := strings.TrimSpace(string(l)); strings.HasPrefix(t, "[") {
			first = i
			break
		}
	}
	var out []byte
	done := false
	for i, l := range lines {
		if i < first && !done && isPriorityLine(l) {
			out = append(out, line...)
			if c := trailingComment(l); c != "" {
				out = append(out, ' ')
				out = append(out, c...)
			}
			out = append(out, lineEnd(l, eol)...)
			done = true
			continue
		}
		out = append(out, l...)
	}
	if !done {
		// Insert after the last top-level line that is not blank.
		at := first
		for at > 0 && len(bytes.TrimSpace(lines[at-1])) == 0 {
			at--
		}
		out = out[:0]
		for _, l := range lines[:at] {
			out = append(out, l...)
		}
		if len(out) > 0 && !bytes.HasSuffix(out, []byte("\n")) {
			out = append(out, eol...)
		}
		out = append(out, line+eol...)
		if at == first && first < len(lines) {
			out = append(out, eol...) // a blank line before the table
		}
		for _, l := range lines[at:] {
			out = append(out, l...)
		}
	}
	var after map[string]any
	if _, err := toml.Decode(string(out), &after); err != nil {
		return nil, fmt.Errorf("%s: cannot set priority safely (%v); edit it by hand", FileName, err)
	}
	got, ok := after["priority"].(int64)
	delete(before, "priority")
	delete(after, "priority")
	if !ok || got != v || !reflect.DeepEqual(before, after) {
		return nil, fmt.Errorf("%s: cannot set priority safely; edit it by hand", FileName)
	}
	return out, nil
}

func isPriorityLine(l []byte) bool {
	t := strings.TrimSpace(string(l))
	k, _, ok := strings.Cut(t, "=")
	if !ok {
		return false
	}
	k = strings.Trim(strings.TrimSpace(k), `"'`)
	return k == "priority"
}

// trailingComment returns the "# …" after a priority line's value (an
// integer, a string without '#' or other simple value), or "".
func trailingComment(l []byte) string {
	t := strings.TrimRight(string(l), "\r\n")
	_, v, _ := strings.Cut(t, "=")
	if strings.ContainsAny(v, `"'`) {
		return "" // a string value may hold '#': drop the comment rather than guess
	}
	if i := strings.Index(v, "#"); i >= 0 {
		return strings.TrimSpace(v[i:])
	}
	return ""
}

func lineEnd(l []byte, eol string) string {
	switch {
	case bytes.HasSuffix(l, []byte("\r\n")):
		return "\r\n"
	case bytes.HasSuffix(l, []byte("\n")):
		return "\n"
	default:
		return eol
	}
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
