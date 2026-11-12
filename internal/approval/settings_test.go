package approval

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func status(t *testing.T, f Files, e *Entry) Status {
	t.Helper()
	st, err := JudgeWith(f, e)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// Only agent and permissions overrides are hashed, sorted by key; model,
// effort, machines and other keys are not.
func TestSettingsLines(t *testing.T) {
	data := `title = "Login"
machines = ["studio"]

[steps.review]
permissions = "readonly"
agent = "claude"
model = "opus"

[steps.build]
agent = "codex"
effort = "high"

[approval]
hash = "sha256:x"
`
	lines, err := SettingsLines([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"step.build.agent=codex", "step.review.agent=claude", "step.review.permissions=readonly"}
	if !slices.Equal(lines, want) {
		t.Errorf("lines %q, want %q", lines, want)
	}
	if none, _ := SettingsLines(nil); len(none) != 0 {
		t.Errorf("no sidecar: %q", none)
	}
	if h := SettingsHash(want); h == SettingsHash(nil) || !strings.HasPrefix(h, Prefix) {
		t.Errorf("hash %s", h)
	}
	if _, err := SettingsLines([]byte("[steps.build]\nagent = 1\n")); err == nil {
		t.Error("a non-string agent was hashed")
	}
}

// change-settings, scenario Agent changed after approval: approve writes
// the settings hash, and a new agent override makes the approval stale,
// naming the override; the text hash still matches. Unapprove removes the
// approval with its settings hash.
func TestAgentChangedAfterApproval(t *testing.T) {
	d := copyFixture(t, false)
	write(t, d, SidecarName, "depends_on = []\n")
	a, err := Approve(d, "Sam", at)
	if err != nil {
		t.Fatal(err)
	}
	if a.SettingsHash != SettingsHash(nil) || !strings.Contains(read(t, d, SidecarName), "settings_hash = \""+a.SettingsHash+"\"") {
		t.Fatalf("settings hash not written:\n%s", read(t, d, SidecarName))
	}
	entry := &Entry{State: EntryApproved, Hash: a.Hash, SettingsHash: a.SettingsHash, By: "Sam", At: at}
	for _, e := range []*Entry{nil, entry} {
		if st := status(t, d, e); st.State != Approved {
			t.Fatalf("before the override (entry %v): %+v", e != nil, st)
		}
	}

	side := read(t, d, SidecarName) + "\n[steps.build]\nagent = \"codex\"\n"
	write(t, d, SidecarName, side)
	for _, e := range []*Entry{nil, entry} {
		st := status(t, d, e)
		if st.State != Stale || st.TextStale || !slices.Equal(st.SettingsChanged, []string{"step.build.agent"}) {
			t.Errorf("after the override (entry %v): %+v", e != nil, st)
		}
		if r := st.StaleReason(); r != "its step.build.agent changed" {
			t.Errorf("reason %q", r)
		}
	}

	// Approving again covers the override; changing it names it again.
	a, err = Approve(d, "Sam", at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(a.Settings, []string{"step.build.agent=codex"}) || judge(t, d) != Approved {
		t.Fatalf("re-approve: %+v %s", a, read(t, d, SidecarName))
	}
	write(t, d, SidecarName, strings.Replace(read(t, d, SidecarName), `agent = "codex"`, "agent = \"claude\"\npermissions = \"edit\"", 1))
	st := status(t, d, nil)
	if st.State != Stale || !slices.Equal(st.SettingsChanged, []string{"step.build.agent", "step.build.permissions"}) {
		t.Errorf("agent and permissions changed: %+v", st)
	}
	// Both hashes off.
	write(t, d, "design.md", "edited\n")
	if r := status(t, d, nil).StaleReason(); r != "its text changed and its step.build.agent, step.build.permissions changed" {
		t.Errorf("reason %q", r)
	}

	// Unapprove removes the approval, settings hash and all.
	if removed, err := Unapprove(d); err != nil || !removed {
		t.Fatal(removed, err)
	}
	if got := read(t, d, SidecarName); strings.Contains(got, "settings_hash") || strings.Contains(got, "[approval]") {
		t.Errorf("after unapprove:\n%s", got)
	}
	if judge(t, d) != None {
		t.Error("not none after unapprove")
	}
	// A withdrawal on the ref carries no settings hash and is none.
	if st := status(t, d, &Entry{State: EntryWithdrawn, By: "Sam", At: at}); st.State != None || st.SettingsChanged != nil {
		t.Errorf("withdrawn: %+v", st)
	}
}

// change-settings, scenario Old approval without overrides: an [approval]
// (or ref entry) with no settings_hash, on a change with no step overrides,
// is still approved; adding an agent or permissions override then makes it
// stale.
func TestOldApprovalWithoutOverrides(t *testing.T) {
	d := copyFixture(t, false)
	hash := mustHash(t, d)
	write(t, d, SidecarName, "depends_on = []\n\n[steps.build]\nmodel = \"opus\"\n\n[approval]\nhash = \""+hash+"\"\nby = \"Old\"\nat = 2026-01-01T00:00:00Z\n")
	old := &Entry{State: EntryApproved, Hash: hash, By: "Old", At: at}
	for _, e := range []*Entry{nil, old} {
		if st := status(t, d, e); st.State != Approved || st.SettingsChanged != nil {
			t.Errorf("old approval (entry %v): %+v", e != nil, st)
		}
	}
	write(t, d, SidecarName, strings.Replace(read(t, d, SidecarName), "model = \"opus\"", "permissions = \"edit\"", 1))
	for _, e := range []*Entry{nil, old} {
		if st := status(t, d, e); st.State != Stale || !slices.Equal(st.SettingsChanged, []string{"step.build.permissions"}) {
			t.Errorf("old approval, override added (entry %v): %+v", e != nil, st)
		}
	}
}

// change-settings, scenario Model changed after approval: changing model,
// effort or machines changes neither hash, so the approval stays.
func TestModelChangedAfterApproval(t *testing.T) {
	d := copyFixture(t, false)
	base := "[steps.build]\nagent = \"codex\"\nmodel = \"gpt-5.5\"\n"
	write(t, d, SidecarName, base)
	a, err := Approve(d, "Sam", at)
	if err != nil {
		t.Fatal(err)
	}
	entry := &Entry{State: EntryApproved, Hash: a.Hash, SettingsHash: a.SettingsHash, Settings: a.Settings, By: "Sam", At: at}
	approved := read(t, d, SidecarName)
	for name, edit := range map[string]func(string) string{
		"model": func(s string) string { return strings.Replace(s, `model = "gpt-5.5"`, `model = "sonnet"`, 1) },
		"effort": func(s string) string {
			return strings.Replace(s, `model = "gpt-5.5"`, "model = \"gpt-5.5\"\neffort = \"high\"", 1)
		},
		"machines": func(s string) string { return "machines = [\"studio\", \"mini\"]\n" + s },
		"model removed": func(s string) string {
			return strings.Replace(s, "model = \"gpt-5.5\"\n", "", 1)
		},
	} {
		write(t, d, SidecarName, edit(approved))
		for _, e := range []*Entry{nil, entry} {
			st := status(t, d, e)
			if st.State != Approved || st.SettingsHash != a.SettingsHash || st.Hash != a.Hash {
				t.Errorf("%s changed (entry %v): %+v", name, e != nil, st)
			}
		}
	}
}

// A removed override is named; an entry whose recorded settings don't
// match its hash names every override now.
func TestSettingsChangedNames(t *testing.T) {
	d := copyFixture(t, false)
	write(t, d, SidecarName, "[steps.review]\nagent = \"claude\"\n")
	hash := mustHash(t, d)
	lines := []string{"step.review.agent=claude"}
	e := &Entry{State: EntryApproved, Hash: hash, SettingsHash: SettingsHash(lines), Settings: lines, By: "S", At: at}
	write(t, d, SidecarName, "[steps.build]\nagent = \"codex\"\n")
	if st := status(t, d, e); !slices.Equal(st.SettingsChanged, []string{"step.build.agent", "step.review.agent"}) {
		t.Errorf("added and removed: %+v", st)
	}
	e.Settings = nil // not recorded
	if st := status(t, d, e); !slices.Equal(st.SettingsChanged, []string{"step.build.agent"}) {
		t.Errorf("unrecorded: %+v", st)
	}
	write(t, d, SidecarName, "depends_on = []\n")
	if st := status(t, d, e); st.State != Stale || len(st.SettingsChanged) != 1 {
		t.Errorf("unrecorded, all removed: %+v", st)
	}
}
