package scope

import (
	"strings"
	"testing"
)

const tasksMD = "# Tasks\n\n" +
	"## 1. Choosing a scope\n\n" +
	"- [x] 1.1 Parse it\n" +
	"- [ ] 1.2 Refuse bad ones\n" +
	"- [ ] 1.3 Test one group\n" +
	"- [ ] 1.4 Test a range\n\n" +
	"## 2. Engine\n\n" +
	"- [ ] 2.1 Add the field\n" +
	"- [ ] 2.2 Start again\n" +
	"- [ ] 2.3 Check ticks\n\n" +
	"```\n- [ ] 9.1 Not a task, in a fence\n```\n\n" +
	"## 3. Wrap-up\n\n" +
	"- [x] 3.1 README\n" +
	"- [ ] 3.2 (ship) Archive this change\n"

func resolve(t *testing.T, text, spec string) (*Scope, error) {
	t.Helper()
	return Resolve(spec, Parse(text))
}

func want(t *testing.T, s *Scope, err error, spec string, tasks ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if s.Spec != spec || strings.Join(s.Tasks, " ") != strings.Join(tasks, " ") {
		t.Errorf("scope %s %v, want %s %v", s.Spec, s.Tasks, spec, tasks)
	}
}

func refused(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), msg) {
		t.Errorf("error %v, want one containing %q", err, msg)
	}
}

func TestParse(t *testing.T) {
	tasks := Parse(strings.ReplaceAll(tasksMD, "\n", "\r\n"))
	if len(tasks) != 9 {
		t.Fatalf("%d tasks: %+v", len(tasks), tasks)
	}
	first, last := tasks[0], tasks[8]
	if first.Number != "1.1" || first.Group != 1 || first.Text != "Parse it" || !first.Checked || first.Ship {
		t.Errorf("first %+v", first)
	}
	if last.Number != "3.2" || last.Group != 3 || !last.Ship || last.Checked {
		t.Errorf("last %+v", last)
	}
	groups := Groups(tasksMD)
	if len(groups) != 3 || groups[0].Title != "Choosing a scope" || len(groups[0].Tasks) != 4 || groups[2].Number != 3 {
		t.Errorf("groups %+v", groups)
	}
}

// Scenario: Run one group.
func TestRunOneGroup(t *testing.T) {
	s, err := resolve(t, tasksMD, "2")
	want(t, s, err, "2", "2.1", "2.2", "2.3")
	s, err = resolve(t, tasksMD, "1.2")
	want(t, s, err, "1.2", "1.2")
}

// Scenario: Range across groups.
func TestRangeAcrossGroups(t *testing.T) {
	s, err := resolve(t, tasksMD, "1.3-2.1")
	want(t, s, err, "1.3-2.1", "1.3", "1.4", "2.1")
}

// Scenario: Next group. Group 1 is all ticked; next is group 2, stored as
// "2".
func TestNextGroup(t *testing.T) {
	text := strings.NewReplacer("- [ ] 1.", "- [x] 1.").Replace(tasksMD)
	s, err := resolve(t, text, Next)
	want(t, s, err, "2", "2.1", "2.2", "2.3")
	// The first unticked task decides, whatever group order says.
	s, err = resolve(t, tasksMD, Next)
	want(t, s, err, "1", "1.1", "1.2", "1.3", "1.4")
	// Ship tasks never make a group next.
	all := strings.NewReplacer("- [ ] 1.", "- [x] 1.", "- [ ] 2.", "- [x] 2.").Replace(tasksMD)
	_, err = resolve(t, all, Next)
	refused(t, err, "every task in tasks.md is ticked")
}

// Scenario: Unknown task. Also unknown tasks in ranges, reversed ranges
// and malformed scopes.
func TestUnknownTask(t *testing.T) {
	_, err := resolve(t, tasksMD, "7")
	refused(t, err, "tasks.md has no group 7")
	_, err = resolve(t, tasksMD, "2.9")
	refused(t, err, "tasks.md has no task 2.9")
	_, err = resolve(t, tasksMD, "1.2-2.9")
	refused(t, err, "tasks.md has no task 2.9")
	_, err = resolve(t, tasksMD, "2.2-1.3")
	refused(t, err, "the range ends before it starts")
	for _, bad := range []string{"", "x", "1-2", "1.", "1.2,1.3", "1.2-"} {
		if err := Valid(bad); err == nil {
			t.Errorf("Valid(%q) passed", bad)
		}
	}
	// A fenced line is not a task.
	_, err = resolve(t, tasksMD, "9")
	refused(t, err, "tasks.md has no group 9")
	_, err = resolve(t, "- [ ] 1.1 a\n- [ ] 1.1 b\n", "1")
	refused(t, err, "appears more than once")
}

// Scenario: Scope already done.
func TestScopeAlreadyDone(t *testing.T) {
	text := strings.NewReplacer("- [ ] 1.", "- [x] 1.").Replace(tasksMD)
	_, err := resolve(t, text, "1")
	refused(t, err, "the tasks in scope 1 are all ticked")
	_, err = resolve(t, text, "1.2-1.4")
	refused(t, err, "the tasks in scope 1.2-1.4 are all ticked")
}

// Scenario: Scope of a ship task. Ship tasks are left out of a group.
func TestScopeOfShipTask(t *testing.T) {
	_, err := resolve(t, tasksMD, "3.2")
	refused(t, err, "3.2 is a ship task: it is done by shipping")
	_, err = resolve(t, "## 6. End\n\n- [ ] 6.1 (ship) Archive\n- [ ] 6.2 (ship) Close\n", "6")
	refused(t, err, "ship tasks: they are done by shipping")
	// Group 3 has only 3.1 left in scope, and it is ticked.
	_, err = resolve(t, tasksMD, "3")
	refused(t, err, "all ticked")
	s, err := resolve(t, strings.Replace(tasksMD, "- [x] 3.1", "- [ ] 3.1", 1), "3")
	want(t, s, err, "3", "3.1")
}

func TestCheckAndUnticked(t *testing.T) {
	s := &Scope{Spec: "2", Tasks: []string{"2.1", "2.2", "2.4"}}
	text := strings.Replace(tasksMD, "- [ ] 2.1", "- [x] 2.1", 1)
	unticked, missing := Check(s, Parse(text))
	if strings.Join(unticked, " ") != "2.2" || strings.Join(missing, " ") != "2.4" {
		t.Errorf("unticked %v missing %v", unticked, missing)
	}
	if got := strings.Join(Unticked(Parse(tasksMD)), " "); got != "1.2 1.3 1.4 2.1 2.2 2.3" {
		t.Errorf("unticked %s", got)
	}
	all := strings.NewReplacer("- [ ] 1.", "- [x] 1.", "- [ ] 2.", "- [x] 2.").Replace(tasksMD)
	if got := Unticked(Parse(all)); len(got) != 0 {
		t.Errorf("complete change has unticked %v (ship tasks don't count)", got)
	}
}

func TestTickShipPreservesFencesAndLineEndings(t *testing.T) {
	text := "## 1. Build\r\n- [x] 1.1 done\r\n- [ ] 1.2 ordinary\r\n- [ ] 1.3 (ship) archive\r\n```md\r\n- [ ] 1.3 (ship) example\r\n```\r\n"
	got, err := TickShip(text)
	want := strings.Replace(text, "- [ ] 1.3 (ship) archive", "- [x] 1.3 (ship) archive", 1)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	if _, err := TickShip("- [ ] 1.1 (ship) first\n- [ ] 1.1 ordinary duplicate\n"); err == nil {
		t.Fatal("ambiguous task number accepted")
	}
}
