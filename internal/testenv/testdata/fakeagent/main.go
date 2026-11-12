// Command fakeagent stands in for an agent CLI in the runs tests. It reads
// its step from the TUCKERBOX_* environment and what to do from the JSON
// plan file named by FAKEAGENT_PLAN:
//
//	{"<change>:<step>:<round>": action, "<change>:<step>": action,
//	 "<step>:<round>": action, "<step>": action, "*": action}
//
// The most specific key wins; with no match it reports done. Every start
// is appended to FAKEAGENT_RECORD as "<change> <step> <round> <pid>".
//
// Given a turn hook it acts as an interactive agent CLI in a terminal: it
// reports each turn's final message through the hook rather than a result
// file, reads the person's answers from standard input, and keeps running
// after its last turn. It finds the hook as each CLI is given it:
//
//	--settings <json>        Claude Code: the Stop hook command, run by the
//	                         shell with a payload naming a transcript
//	-c notify=[...]          Codex: argv, run with a notify payload as the
//	                         last argument
//	--notify <argv...> --    a custom agent's {turnhook}, run like notify
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type finding struct {
	Severity string `json:"severity"`
	File     string `json:"file"`
	Detail   string `json:"detail"`
}

type action struct {
	// Status to report (default done); "none" writes no result.
	Status   string    `json:"status"`
	Summary  string    `json:"summary"`
	Findings []finding `json:"findings"`
	// WaitFor waits until this file exists before reporting.
	WaitFor string `json:"wait_for"`
	// Touch writes this file (relative to the worktree) before reporting.
	Touch string `json:"touch"`
	// Exit is the exit status.
	Exit int `json:"exit"`
	// StaleFirst writes a result for the previous round first.
	StaleFirst bool `json:"stale_first"`
	// Message reports the result as the final message on stdout instead of
	// writing the result file, as a sandboxed agent does.
	Message bool `json:"message"`
	// Tick ticks these task numbers in the change's tasks.md.
	Tick []string `json:"tick"`
	// Edit appends a line to this file in the change folder.
	Edit string `json:"edit"`
	// Ask (interactive) ends the first turn with this question and reads
	// the answer from standard input; the summary then quotes it.
	Ask string `json:"ask"`
	// ExitAfter (interactive) exits after the last turn.
	ExitAfter bool `json:"exit_after"`
	// FileFirst (interactive) writes the result file mid-turn, then ends
	// that turn a second later with "Done." and no result.
	FileFirst bool `json:"file_first"`
}

// hook is how the fake agent hands a turn's final message over.
type hook struct {
	style string // "claude", "notify"
	shell string
	argv  []string
}

// findHook finds a turn hook in the arguments, or returns nil.
func findHook(args []string) *hook {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--settings" && i+1 < len(args):
			var v struct {
				Hooks struct {
					Stop []struct {
						Hooks []struct {
							Command string `json:"command"`
						} `json:"hooks"`
					} `json:"Stop"`
				} `json:"hooks"`
			}
			must(json.Unmarshal([]byte(args[i+1]), &v))
			return &hook{style: "claude", shell: v.Hooks.Stop[0].Hooks[0].Command}
		case args[i] == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "notify="):
			var argv []string
			must(json.Unmarshal([]byte(strings.TrimPrefix(args[i+1], "notify=")), &argv))
			return &hook{style: "notify", argv: argv}
		case args[i] == "--notify":
			h := &hook{style: "notify"}
			for _, a := range args[i+1:] {
				if a == "--" {
					break
				}
				h.argv = append(h.argv, a)
			}
			return h
		}
	}
	return nil
}

// turn ends a turn with msg as its final message.
func (h *hook) turn(msg string) {
	switch h.style {
	case "claude":
		dir, err := os.MkdirTemp("", "fakeagent-transcript")
		must(err)
		defer os.RemoveAll(dir)
		tp := filepath.Join(dir, "session.jsonl")
		var lines []string
		for _, e := range []map[string]any{
			{"type": "user", "message": map[string]any{"role": "user", "content": "go"}},
			{"type": "assistant", "message": map[string]any{"id": "m1", "role": "assistant", "content": []map[string]any{{"type": "text", "text": "Working on it."}}}},
			{"type": "assistant", "message": map[string]any{"id": "m2", "role": "assistant", "content": []map[string]any{{"type": "text", "text": msg}}}},
		} {
			data, _ := json.Marshal(e)
			lines = append(lines, string(data))
		}
		must(os.WriteFile(tp, []byte(strings.Join(lines, "\n")+"\n"), 0o644))
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "transcript_path": tp, "session_id": "s1"})
		cmd := exec.Command("sh", "-c", h.shell)
		cmd.Stdin = strings.NewReader(string(payload))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		must(cmd.Run())
	default:
		payload, _ := json.Marshal(map[string]any{"type": "agent-turn-complete", "turn-id": "1", "last-assistant-message": msg})
		cmd := exec.Command(h.argv[0], append(h.argv[1:], string(payload))...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		must(cmd.Run())
	}
}

func main() {
	change := os.Getenv("TUCKERBOX_CHANGE")
	step := os.Getenv("TUCKERBOX_STEP")
	roundS := os.Getenv("TUCKERBOX_ROUND")
	round, _ := strconv.Atoi(roundS)
	resultPath := os.Getenv("TUCKERBOX_RESULT")

	if rec := os.Getenv("FAKEAGENT_RECORD"); rec != "" {
		f, err := os.OpenFile(rec, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		must(err)
		fmt.Fprintf(f, "%s %s %d %d\n", change, step, round, os.Getpid())
		f.Close()
	}
	var a action
	if p := os.Getenv("FAKEAGENT_PLAN"); p != "" {
		data, err := os.ReadFile(p)
		must(err)
		plan := map[string]action{}
		must(json.Unmarshal(data, &plan))
		for _, k := range []string{change + ":" + step + ":" + roundS, change + ":" + step, step + ":" + roundS, step, "*"} {
			if v, ok := plan[k]; ok {
				a = v
				break
			}
		}
	}
	fmt.Printf("fakeagent: %s %s round %d\n", change, step, round)
	if a.WaitFor != "" {
		for {
			if _, err := os.Stat(a.WaitFor); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if a.Touch != "" {
		wd, _ := os.Getwd()
		p := filepath.Join(wd, filepath.FromSlash(a.Touch))
		must(os.MkdirAll(filepath.Dir(p), 0o755))
		must(os.WriteFile(p, []byte(step+" round "+roundS+"\n"), 0o644))
	}
	if len(a.Tick) > 0 || a.Edit != "" {
		wd, _ := os.Getwd()
		dir := filepath.Join(wd, "openspec", "changes", change)
		if len(a.Tick) > 0 {
			p := filepath.Join(dir, "tasks.md")
			data, err := os.ReadFile(p)
			must(err)
			text := string(data)
			for _, n := range a.Tick {
				text = strings.Replace(text, "- [ ] "+n+" ", "- [x] "+n+" ", 1)
			}
			must(os.WriteFile(p, []byte(text), 0o644))
		}
		if a.Edit != "" {
			p := filepath.Join(dir, filepath.FromSlash(a.Edit))
			data, _ := os.ReadFile(p)
			must(os.WriteFile(p, append(data, []byte("\nEdited by "+step+".\n")...), 0o644))
		}
	}
	status := a.Status
	if status == "" {
		status = "done"
	}
	summary := a.Summary
	if summary == "" {
		summary = "fake " + step + " " + status
	}
	if h := findHook(os.Args[1:]); h != nil {
		interactive(h, a, change, step, round, status, summary)
		return
	}
	write := func(r int) {
		data, _ := json.Marshal(map[string]any{
			"change": change, "step": step, "round": r, "status": status,
			"summary": summary, "findings": a.Findings,
		})
		tmp := resultPath + ".tmp"
		must(os.WriteFile(tmp, data, 0o644))
		var err error
		for i := 0; i < 50; i++ {
			if err = os.Rename(tmp, resultPath); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		must(err)
	}
	if a.StaleFirst {
		write(round - 1)
		time.Sleep(700 * time.Millisecond)
	}
	if status != "none" && a.Message {
		data, _ := json.MarshalIndent(map[string]any{
			"change": change, "step": step, "round": round, "status": status,
			"summary": summary, "findings": a.Findings,
		}, "", "  ")
		fmt.Printf("Finished.\n\n```json\n%s\n```\n", data)
	} else if status != "none" {
		write(round)
	} else {
		fmt.Fprintln(os.Stderr, "fakeagent: giving up without a result")
	}
	os.Exit(a.Exit)
}

// interactive is the fake agent as an interactive CLI in a terminal.
func interactive(h *hook, a action, change, step string, round int, status, summary string) {
	in := bufio.NewScanner(os.Stdin)
	resultJSON := func(r int) []byte {
		data, _ := json.MarshalIndent(map[string]any{
			"change": change, "step": step, "round": r, "status": status,
			"summary": summary, "findings": a.Findings,
		}, "", "  ")
		return data
	}
	result := func(r int) string {
		return "Finished.\n\n```json\n" + string(resultJSON(r)) + "\n```"
	}
	if a.Ask != "" {
		fmt.Println(a.Ask)
		h.turn(a.Ask)
		if !in.Scan() {
			os.Exit(1)
		}
		fmt.Println("fakeagent: the person said:", in.Text())
		summary = "answer: " + in.Text()
	}
	if a.StaleFirst {
		h.turn(result(round - 1))
		time.Sleep(700 * time.Millisecond)
	}
	switch {
	case status == "none":
		h.turn("I could not finish this.")
	case a.FileFirst:
		p := os.Getenv("TUCKERBOX_RESULT")
		must(os.WriteFile(p+".tmp", resultJSON(round), 0o644))
		must(os.Rename(p+".tmp", p))
		fmt.Println("fakeagent: wrote the result file")
		time.Sleep(time.Second)
		h.turn("Done.")
	default:
		h.turn(result(round))
	}
	if a.ExitAfter {
		os.Exit(a.Exit)
	}
	for in.Scan() {
		fmt.Println("fakeagent: the person said:", in.Text())
	}
	// An interactive CLI waits for its next prompt until it is ended.
	for {
		time.Sleep(time.Hour)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		os.Exit(1)
	}
}
