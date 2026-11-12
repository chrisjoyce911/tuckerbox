// Package priority reads and orders the owner's rank of OpenSpec changes
// (change-priority). A change may have
//
//	priority = 1000
//
// in its openspec/changes/<id>/tuckerbox.toml. Lower comes first; a change
// without one comes after every ranked change. The value is an ordinal rank
// that Tuckerbox chooses (with gaps, see [Plan]); people use positions.
//
// There are two orders. The priority order (priority, then age, then id) is
// total and orders the list and the board. The admission order (see
// [Admission]) decides which queued run starts next: it uses priority
// values, inherited from dependents, then request order, never age.
//
// The package reads and edits bytes only; it takes no locks. Callers hold
// the repository's sidecar lock (runs.WithSidecarLock) around reads that
// decide admission and around every write.
package priority

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/deps"
)

// FileName is the sidecar in a change folder that holds priority.
const FileName = "tuckerbox.toml"

// Gap is the distance between the values Tuckerbox adds.
const Gap = 1000

// Change is what ordering needs to know about one active change.
type Change struct {
	ID string
	// Priority is the stored value, or 0 for none (also for a value that
	// is not a positive integer: ordering treats it as none).
	Priority int64
	// Created is the change's age (created in .openspec.yaml); zero when
	// unknown.
	Created time.Time
	// DependsOn are the changes it depends on.
	DependsOn []string
}

// Ranked reports whether the change has a priority.
func (c Change) Ranked() bool { return c.Priority > 0 }

// Problem is an invalid priority found by [Check].
type Problem struct {
	// File is the tuckerbox.toml, relative to the checkout, with forward
	// slashes.
	File   string
	Change string
	Msg    string
}

func (p Problem) String() string { return p.File + ": priority: " + p.Msg }

// File returns the path of change id's tuckerbox.toml in the checkout at
// root.
func File(root, id string) string { return filepath.Join(deps.ChangesDir(root), id, FileName) }

// RelFile is change id's tuckerbox.toml relative to the checkout.
func RelFile(id string) string { return "openspec/changes/" + id + "/" + FileName }

// Parse returns the priority in a tuckerbox.toml's contents: 0 when there
// is none. A value that is not a positive integer is an error (and counts
// as none). Invalid TOML is an error too.
func Parse(data []byte) (int64, error) {
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return 0, err
	}
	v, ok := doc["priority"]
	if !ok {
		return 0, nil
	}
	n, ok := v.(int64)
	if !ok || n <= 0 {
		return 0, &ValueError{Value: v}
	}
	return n, nil
}

// ValueError is a priority that is not a positive integer.
type ValueError struct{ Value any }

func (e *ValueError) Error() string {
	var shown string
	switch v := e.Value.(type) {
	case string:
		shown = fmt.Sprintf("%q", v)
	case map[string]any:
		shown = "a table"
	case []any, []map[string]any:
		shown = "an array"
	default:
		shown = fmt.Sprint(v)
	}
	return "must be a positive integer, not " + shown
}

// Read reads change id from the checkout at root. A priority that is not a
// positive integer is returned as an error with the change (Priority 0);
// so is a tuckerbox.toml that is not valid TOML.
func Read(root, id string) (Change, error) {
	c := Change{ID: id, Created: created(filepath.Join(deps.ChangesDir(root), id, ".openspec.yaml"))}
	data, err := os.ReadFile(File(root, id))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var f deps.File
	if _, err := toml.Decode(string(data), &f); err == nil {
		seen := map[string]bool{}
		for _, d := range f.DependsOn {
			if d = strings.TrimSpace(d); d != "" && !seen[d] {
				seen[d] = true
				c.DependsOn = append(c.DependsOn, d)
			}
		}
	}
	p, err := Parse(data)
	c.Priority = p
	return c, err
}

// Load reads every active change in the checkout at root, in id order.
// Changes whose priority can't be read count as unranked; Load does not
// report them (see [Check]).
func Load(root string) ([]Change, error) {
	ids, err := deps.Active(root)
	if err != nil {
		return nil, err
	}
	out := make([]Change, 0, len(ids))
	for _, id := range ids {
		c, _ := Read(root, id)
		out = append(out, c)
	}
	return out, nil
}

// Check returns each active change in the checkout at root whose priority
// is not a positive integer. A tuckerbox.toml that is not valid TOML is
// left to the dependency check, which reports it.
func Check(root string) ([]Problem, error) {
	ids, err := deps.Active(root)
	if err != nil {
		return nil, err
	}
	var out []Problem
	for _, id := range ids {
		_, err := Read(root, id)
		var ve *ValueError
		if errors.As(err, &ve) {
			out = append(out, Problem{File: RelFile(id), Change: id, Msg: ve.Error()})
		}
	}
	return out, nil
}

// created reads the created date from a change's .openspec.yaml; zero when
// it has none.
func created(path string) time.Time {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		v, ok := strings.CutPrefix(line, "created:")
		if !ok {
			continue
		}
		if i := strings.Index(v, " #"); i >= 0 {
			v = v[:i]
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
		return time.Time{}
	}
	return time.Time{}
}

// olderFirst compares ages: known before unknown, older first. It returns
// -1, 0 or 1.
func olderFirst(a, b time.Time) int {
	switch {
	case a.Equal(b):
		return 0
	case a.IsZero():
		return 1
	case b.IsZero():
		return -1
	case a.Before(b):
		return -1
	default:
		return 1
	}
}

// Less is the priority order: ranked before unranked, lower value first,
// then older, then by id.
func Less(a, b Change) bool {
	if a.Ranked() != b.Ranked() {
		return a.Ranked()
	}
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	if c := olderFirst(a.Created, b.Created); c != 0 {
		return c < 0
	}
	return a.ID < b.ID
}

// Sort returns the changes in priority order. Position n (from 1) is index
// n-1.
func Sort(cs []Change) []Change {
	out := append([]Change(nil), cs...)
	sort.SliceStable(out, func(i, j int) bool { return Less(out[i], out[j]) })
	return out
}

// Positions maps each change id to its position in priority order, from 1.
func Positions(cs []Change) map[string]int {
	out := map[string]int{}
	for i, c := range Sort(cs) {
		out[c.ID] = i + 1
	}
	return out
}

// none is the admission rank of a change without a priority: after every
// value.
const none = int64(math.MaxInt64)

// Request is one change to admit, with its request order: the queue
// ticket's time, or for tuckerbox run --all, its place in age order.
type Request struct {
	ID string
	At int64
}

// Ranks returns each change's admission rank: the lowest priority value of
// it and every change in all that depends on it, directly or not. A change
// with no value anywhere ranks after every value (math.MaxInt64).
func Ranks(all []Change) map[string]int64 {
	byID := map[string]Change{}
	dependents := map[string][]string{}
	for _, c := range all {
		byID[c.ID] = c
		for _, d := range c.DependsOn {
			dependents[d] = append(dependents[d], c.ID)
		}
	}
	out := map[string]int64{}
	for _, c := range all {
		best := none
		seen := map[string]bool{c.ID: true}
		stack := []string{c.ID}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if p := byID[id].Priority; p > 0 && p < best {
				best = p
			}
			for _, up := range dependents[id] {
				if !seen[up] {
					seen[up] = true
					stack = append(stack, up)
				}
			}
		}
		out[c.ID] = best
	}
	return out
}

// Admission orders reqs for admission, one change at a time: next is the
// change with the lowest (rank, request, id) among those whose
// dependencies in reqs, direct or through changes outside reqs, are
// already placed. all holds every active change (for ranks and the
// dependency graph); a request for a change not in all ranks after every
// value and has no dependencies. A dependency cycle among the requests
// (which tuckerbox check reports) does not stall the order: the rest are
// placed by (rank, request, id).
func Admission(reqs []Request, all []Change) []Request {
	ranks := Ranks(all)
	byID := map[string]Change{}
	for _, c := range all {
		byID[c.ID] = c
	}
	rank := func(id string) int64 {
		if r, ok := ranks[id]; ok {
			return r
		}
		return none
	}
	in := map[string]bool{}
	for _, r := range reqs {
		in[r.ID] = true
	}
	// needs[id]: the requested changes id depends on, directly or not.
	needs := map[string]map[string]bool{}
	for _, r := range reqs {
		n := map[string]bool{}
		seen := map[string]bool{r.ID: true}
		stack := append([]string(nil), byID[r.ID].DependsOn...)
		for len(stack) > 0 {
			d := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[d] {
				continue
			}
			seen[d] = true
			if in[d] {
				n[d] = true
			}
			stack = append(stack, byID[d].DependsOn...)
		}
		needs[r.ID] = n
	}
	less := func(a, b Request) bool {
		ra, rb := rank(a.ID), rank(b.ID)
		if ra != rb {
			return ra < rb
		}
		if a.At != b.At {
			return a.At < b.At
		}
		return a.ID < b.ID
	}
	placed := map[string]bool{}
	left := append([]Request(nil), reqs...)
	out := make([]Request, 0, len(reqs))
	for len(left) > 0 {
		best, eligible := -1, false
		for i, r := range left {
			ok := true
			for d := range needs[r.ID] {
				if !placed[d] {
					ok = false
					break
				}
			}
			switch {
			case ok && !eligible:
				best, eligible = i, true
			case ok == eligible && (best < 0 || less(r, left[best])):
				best = i
			}
		}
		r := left[best]
		placed[r.ID] = true
		out = append(out, r)
		left = append(left[:best], left[best+1:]...)
	}
	return out
}
