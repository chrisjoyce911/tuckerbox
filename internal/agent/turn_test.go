package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var hookArgv = []string{"/opt/tb/tuckerbox", "agent-turn", "--state", "/s t/x", "--step", "build", "--round", "2"}

func TestBuiltinsHaveInteractiveCommands(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		b := Builtin()[name]
		if !b.HasInteractive() {
			t.Fatalf("%s has no interactive command", name)
		}
		if err := b.ValidateInteractive(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	argv, err := Builtin()["claude"].ExpandInteractive(Values{Brief: "read it", Model: "opus", Permissions: []string{"--x"}, TurnHook: hookArgv})
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "claude" || slices.Contains(argv, "-p") || argv[len(argv)-1] != "read it" {
		t.Errorf("claude argv %q", argv)
	}
	if slices.Contains(argv, "--effort") {
		t.Errorf("an unset effort was passed: %q", argv)
	}
	i := slices.Index(argv, "--settings")
	if i < 0 {
		t.Fatalf("no --settings: %q", argv)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal([]byte(argv[i+1]), &settings); err != nil {
		t.Fatal(err)
	}
	stop := settings.Hooks["Stop"]
	if len(stop) != 1 || stop[0].Hooks[0].Type != "command" ||
		stop[0].Hooks[0].Command != "/opt/tb/tuckerbox agent-turn --state '/s t/x' --step build --round 2" {
		t.Errorf("Stop hook %+v", stop)
	}

	argv, err = Builtin()["codex"].ExpandInteractive(Values{Brief: "read it", Workdir: "/w", Effort: "high", TurnHook: hookArgv})
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != "codex" || slices.Contains(argv, "exec") || argv[len(argv)-1] != "read it" || argv[len(argv)-2] != "--" {
		t.Errorf("codex argv %q", argv)
	}
	i = slices.Index(argv, "-c")
	i += slices.Index(argv[i+1:], "-c") + 1 // the second -c, after model_reasoning_effort
	notify, ok := strings.CutPrefix(argv[i+1], "notify=")
	var got []string
	if !ok || json.Unmarshal([]byte(notify), &got) != nil || !slices.Equal(got, hookArgv) {
		t.Errorf("notify %q, want %q", argv[i+1], hookArgv)
	}
}

func TestTurnHookPlaceholder(t *testing.T) {
	whole := Template{Name: "omp", Interactive: []string{"omp", "--on-turn", PhTurnHook, "--", PhBrief}}
	argv, err := whole.ExpandInteractive(Values{Brief: "b", TurnHook: hookArgv})
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]string{"omp", "--on-turn"}, hookArgv...), "--", "b")
	if !slices.Equal(argv, want) {
		t.Errorf("whole {turnhook}: %q, want %q", argv, want)
	}
	inside := Template{Name: "omp", Interactive: []string{"omp", "--hook=" + PhTurnHook, PhBrief}}
	argv, err = inside.ExpandInteractive(Values{Brief: "b", TurnHook: hookArgv})
	if err != nil {
		t.Fatal(err)
	}
	if argv[1] != "--hook="+ShellJoin(hookArgv) {
		t.Errorf("{turnhook} inside an argument: %q", argv[1])
	}
	if err := (Template{Name: "omp", Interactive: []string{"omp", PhBrief}}).ValidateInteractive(); err == nil {
		t.Error("an interactive command without {turnhook} was accepted")
	}
	if err := (Template{Name: "omp", Command: []string{"omp", PhTurnHook, PhBrief}}).Validate(); err == nil {
		t.Error("{turnhook} in a headless command was accepted")
	}
	if (Template{Name: "omp", Command: []string{"omp", PhBrief}}).HasInteractive() {
		t.Error("a custom agent without interactive has one")
	}
}

func TestShellJoin(t *testing.T) {
	if got := ShellJoin([]string{"a", "b c", "it's", ""}); got != `a 'b c' 'it'\''s' ''` {
		t.Errorf("got %s", got)
	}
}

func TestTurnMessage(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Looking."}]}}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","name":"Read"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result"}]}}`,
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"text","text":"Done."}]}}`,
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"text","text":"{\"change\":\"c\"}"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"id":"s","content":[{"type":"text","text":"subagent"}]}}`,
		`{"type":"system"}`,
	}
	if err := os.WriteFile(tr, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, format string
		args         []string
		stdin, want  string
		bad          bool
	}{
		{"claude transcript", TurnClaude, nil, `{"hook_event_name":"Stop","transcript_path":"` + tr + `"}`, "Done.\n{\"change\":\"c\"}", false},
		{"claude payload message", TurnClaude, nil, `{"transcript_path":"/nope","last_assistant_message":"direct"}`, "direct", false},
		{"claude no transcript", TurnClaude, nil, `{"hook_event_name":"Stop"}`, "", true},
		{"codex", TurnCodex, []string{`{"type":"agent-turn-complete","last-assistant-message":"from codex","input-messages":["x"]}`}, "", "from codex", false},
		{"codex null", TurnCodex, []string{`{"last-assistant-message":null}`}, "", "", false},
		{"codex no payload", TurnCodex, nil, "", "", true},
		{"codex format changed", TurnCodex, []string{`{"message":"x"}`}, "", "", true},
		{"auto codex", TurnAuto, []string{`{"last-assistant-message":"m"}`}, "", "m", false},
		{"auto text arg", TurnAuto, []string{"all", "done"}, "", "all done", false},
		{"auto claude", TurnAuto, nil, `{"transcript_path":"` + tr + `"}`, "Done.\n{\"change\":\"c\"}", false},
		{"auto stdin text", TurnAuto, nil, "plain", "plain", false},
	}
	for _, c := range cases {
		got, err := TurnMessage(c.format, c.args, strings.NewReader(c.stdin))
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("%s: %q, %v; want %q (error %v)", c.name, got, err, c.want, c.bad)
		}
	}
}

func TestWriteLastMessage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "messages", "build-1.txt")
	for _, msg := range []string{"first", "second"} {
		if err := WriteLastMessage(p, msg); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, p); got != msg {
			t.Errorf("got %q", got)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
}
