// Package scope reads a change's tasks.md and resolves a run's task scope:
// one group ("2"), one task ("2.3"), a range of tasks in tasks.md order
// ("2.1-2.4"), or "next", the first group with an unticked task.
//
// A task whose text starts with "(ship)" is a ship task: it is done by
// shipping, so it is never part of a scope or of next, and it does not
// count when deciding whether a change is complete.
package scope

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Next is the scope spec for the first group with an unticked task.
const Next = "next"

// ShipMarker starts the text of a ship task.
const ShipMarker = "(ship)"

// Task is one task line of tasks.md.
type Task struct {
	// Number is the task's number, such as "2.1".
	Number string
	// Group is the number before the first dot.
	Group int
	// Text is the task's text after its number.
	Text    string
	Checked bool
	// Ship is set for a task whose text starts with "(ship)".
	Ship bool
}

// Group is a task group: a "## N. Title" heading and the tasks numbered
// N.M.
type Group struct {
	Number int
	Title  string
	Tasks  []Task
}

// Scope is a run's task scope, as kept in run.json.
type Scope struct {
	// Spec is the scope as typed, with next resolved to a group number.
	Spec string `json:"spec"`
	// Tasks are the task numbers it covers, in tasks.md order.
	Tasks []string `json:"tasks"`
}

// SpecOrEmpty returns s.Spec, or "" for no scope (the whole change).
func (s *Scope) SpecOrEmpty() string {
	if s == nil {
		return ""
	}
	return s.Spec
}

var (
	taskLine = regexp.MustCompile(`^[ \t]*[-*+] \[([ xX])\] (\d+(?:\.\d+)+)(?:[ \t]+(.*?))?[ \t]*\r?$`)
	heading  = regexp.MustCompile(`^##[ \t]+(\d+)\.?[ \t]*(.*?)[ \t]*\r?$`)
	groupRe  = regexp.MustCompile(`^\d+$`)
	taskRe   = regexp.MustCompile(`^\d+(?:\.\d+)+$`)
)

// Parse returns the tasks of a tasks.md in document order. Lines inside
// fenced code blocks are not tasks.
func Parse(text string) []Task {
	tasks, _ := parse(text)
	return tasks
}

// Groups returns the task groups of a tasks.md in document order: each
// "## N. Title" heading, and any group number used by tasks without one.
func Groups(text string) []Group {
	tasks, titles := parse(text)
	var out []Group
	at := map[int]int{}
	add := func(n int) int {
		if i, ok := at[n]; ok {
			return i
		}
		at[n] = len(out)
		out = append(out, Group{Number: n, Title: titles[n]})
		return at[n]
	}
	for _, n := range titleOrder(text) {
		add(n)
	}
	for _, t := range tasks {
		i := add(t.Group)
		out[i].Tasks = append(out[i].Tasks, t)
	}
	return out
}

func titleOrder(text string) []int {
	var out []int
	forLines(text, func(line string) {
		if m := heading.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				out = append(out, n)
			}
		}
	})
	return out
}

func parse(text string) ([]Task, map[int]string) {
	var tasks []Task
	titles := map[int]string{}
	forLines(text, func(line string) {
		if m := heading.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				if _, seen := titles[n]; !seen {
					titles[n] = m[2]
				}
			}
			return
		}
		m := taskLine.FindStringSubmatch(line)
		if m == nil {
			return
		}
		g, err := strconv.Atoi(strings.SplitN(m[2], ".", 2)[0])
		if err != nil {
			return
		}
		text := strings.TrimSpace(m[3])
		tasks = append(tasks, Task{
			Number: m[2], Group: g, Text: text, Checked: m[1] != " ",
			Ship: strings.HasPrefix(text, ShipMarker),
		})
	})
	return tasks, titles
}

// forLines calls f for each line of text outside fenced code blocks.
func forLines(text string, f func(string)) {
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		run := ""
		if len(trimmed) >= 3 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if n >= 3 {
				run = trimmed[:n]
			}
		}
		if fence != "" {
			if run != "" && run[0] == fence[0] && len(run) >= len(fence) && strings.TrimSpace(trimmed[len(run):]) == "" {
				fence = ""
			}
			continue
		}
		if run != "" {
			fence = run
			continue
		}
		f(line)
	}
}

// Error is a scope Resolve refuses.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func refuse(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

// Valid reports whether spec has the form of a scope: N, N.M, N.M-N.M or
// next. It does not look at tasks.md.
func Valid(spec string) error {
	switch {
	case spec == Next, groupRe.MatchString(spec), taskRe.MatchString(spec):
		return nil
	}
	if a, b, ok := strings.Cut(spec, "-"); ok && taskRe.MatchString(a) && taskRe.MatchString(b) {
		return nil
	}
	return refuse("--tasks %q: use a group (2), a task (2.3), a range of tasks (2.1-2.4) or next", spec)
}

// Resolve resolves spec against tasks (from the tasks.md the run will
// use). next becomes the first group with an unticked task. It refuses a
// number tasks.md doesn't have, a range that ends before it starts, a
// scope of only ship tasks, and a scope whose tasks are all ticked.
func Resolve(spec string, tasks []Task) (*Scope, error) {
	if err := Valid(spec); err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i, t := range tasks {
		if _, dup := index[t.Number]; dup {
			return nil, refuse("task %s appears more than once in tasks.md; fix the numbering first", t.Number)
		}
		index[t.Number] = i
	}
	var picked []Task
	switch {
	case spec == Next:
		for _, t := range tasks {
			if !t.Ship && !t.Checked {
				spec = strconv.Itoa(t.Group)
				break
			}
		}
		if spec == Next {
			return nil, refuse("every task in tasks.md is ticked; there is no next group to run")
		}
		fallthrough
	case groupRe.MatchString(spec):
		g, _ := strconv.Atoi(spec)
		for _, t := range tasks {
			if t.Group == g {
				picked = append(picked, t)
			}
		}
		if len(picked) == 0 {
			return nil, refuse("tasks.md has no group %s", spec)
		}
	case taskRe.MatchString(spec):
		i, ok := index[spec]
		if !ok {
			return nil, refuse("tasks.md has no task %s", spec)
		}
		picked = tasks[i : i+1]
	default:
		a, b, _ := strings.Cut(spec, "-")
		i, ok := index[a]
		if !ok {
			return nil, refuse("tasks.md has no task %s", a)
		}
		j, ok := index[b]
		if !ok {
			return nil, refuse("tasks.md has no task %s", b)
		}
		if j < i {
			return nil, refuse("--tasks %s: the range ends before it starts (%s comes before %s in tasks.md)", spec, b, a)
		}
		picked = tasks[i : j+1]
	}
	s := &Scope{Spec: spec, Tasks: []string{}}
	open := false
	for _, t := range picked {
		if t.Ship {
			continue
		}
		s.Tasks = append(s.Tasks, t.Number)
		if !t.Checked {
			open = true
		}
	}
	switch {
	case len(s.Tasks) == 0 && len(picked) == 1:
		return nil, refuse("%s is a ship task: it is done by shipping, not by a run", picked[0].Number)
	case len(s.Tasks) == 0:
		return nil, refuse("the tasks in scope %s are ship tasks: they are done by shipping, not by a run", spec)
	case !open:
		return nil, refuse("the tasks in scope %s are all ticked", spec)
	}
	return s, nil
}

// Check returns the scope's tasks that are not ticked in tasks, and those
// tasks.md no longer has, in scope order.
func Check(s *Scope, tasks []Task) (unticked, missing []string) {
	byNumber := map[string]Task{}
	for _, t := range tasks {
		byNumber[t.Number] = t
	}
	for _, n := range s.Tasks {
		t, ok := byNumber[n]
		switch {
		case !ok:
			missing = append(missing, n)
		case !t.Checked:
			unticked = append(unticked, n)
		}
	}
	return unticked, missing
}

// Unticked returns the change's unticked tasks other than ship tasks, in
// tasks.md order. The change is complete when there are none.
func Unticked(tasks []Task) []string {
	var out []string
	for _, t := range tasks {
		if !t.Ship && !t.Checked {
			out = append(out, t.Number)
		}
	}
	return out
}

// AllTicked is how the task of a run whose tasks are all ticked is shown.
const AllTicked = "all tasks in scope ticked"

// Current returns the task a run is on (agent-activity: The task a run is
// on): the first unticked task of s, or of the whole change when s is nil,
// in tasks.md order, skipping ship tasks. ok is false when every such task
// is ticked (AllTicked).
func Current(tasks []Task, s *Scope) (t Task, ok bool) {
	in := map[string]bool{}
	if s != nil {
		for _, n := range s.Tasks {
			in[n] = true
		}
	}
	for _, t := range tasks {
		if t.Ship || t.Checked || (s != nil && !in[t.Number]) {
			continue
		}
		return t, true
	}
	return Task{}, false
}

// Changed returns the numbers of the tasks whose box differs between
// before and after, in after's order, with whether each is now ticked. A
// task only one of them has is not a change of its box.
func Changed(before, after []Task) (numbers []string, ticked []bool) {
	was := map[string]bool{}
	for _, t := range before {
		was[t.Number] = t.Checked
	}
	seen := map[string]bool{}
	for _, t := range after {
		c, ok := was[t.Number]
		if !ok || seen[t.Number] || c == t.Checked {
			continue
		}
		seen[t.Number] = true
		numbers = append(numbers, t.Number)
		ticked = append(ticked, t.Checked)
	}
	return numbers, ticked
}

// Lookup returns the tasks in tasks whose numbers are in numbers, in
// numbers' order; numbers tasks.md doesn't have are left out.
func Lookup(tasks []Task, numbers []string) []Task {
	byNumber := map[string]Task{}
	for _, t := range tasks {
		byNumber[t.Number] = t
	}
	var out []Task
	for _, n := range numbers {
		if t, ok := byNumber[n]; ok {
			out = append(out, t)
		}
	}
	return out
}
