package scope

import (
	"strings"
	"testing"
)

// agent-activity: The task a run is on, "Builder in group 2".
func TestCurrentBuilderInGroup2(t *testing.T) {
	text := strings.NewReplacer("- [ ] 2.1", "- [x] 2.1", "- [ ] 2.2", "- [x] 2.2").Replace(tasksMD)
	tasks := Parse(text)
	s, err := Resolve("2", tasks)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Current(tasks, s)
	if !ok || got.Number != "2.3" || got.Text != "Check ticks" {
		t.Fatalf("current %+v %v, want 2.3 Check ticks", got, ok)
	}
	// Without a scope the first unticked task of the change counts.
	if got, ok := Current(tasks, nil); !ok || got.Number != "1.2" {
		t.Fatalf("whole change: current %+v %v, want 1.2", got, ok)
	}
}

// agent-activity: The task a run is on, "Everything in scope ticked".
func TestCurrentEverythingInScopeTicked(t *testing.T) {
	tasks := Parse(tasksMD)
	s, err := Resolve("2", tasks)
	if err != nil {
		t.Fatal(err)
	}
	ticked := Parse(strings.ReplaceAll(tasksMD, "- [ ] 2.", "- [x] 2."))
	if got, ok := Current(ticked, s); ok {
		t.Fatalf("current %+v, want none (%s)", got, AllTicked)
	}
	// Ship tasks are never the current task.
	all := Parse(strings.ReplaceAll(tasksMD, "- [ ] ", "- [x] ") + "- [ ] 4.1 (ship) Ship it\n")
	if got, ok := Current(all, nil); ok {
		t.Fatalf("current %+v, want none: only a ship task is unticked", got)
	}
}

func TestChanged(t *testing.T) {
	before := Parse(tasksMD)
	after := Parse(strings.NewReplacer("- [ ] 2.1", "- [x] 2.1", "- [x] 1.1", "- [ ] 1.1").Replace(tasksMD) + "- [x] 5.1 New\n")
	n, ticked := Changed(before, after)
	if strings.Join(n, " ") != "1.1 2.1" || ticked[0] || !ticked[1] {
		t.Fatalf("changed %v %v, want 1.1 unticked and 2.1 ticked", n, ticked)
	}
	if n, _ := Changed(before, before); len(n) != 0 {
		t.Fatalf("no change gave %v", n)
	}
}
