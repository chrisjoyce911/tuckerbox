// Command fakeagent stands in for an agent CLI in the runs tests. It reads
// its step from the TUCKERBOX_* environment and what to do from the JSON
// plan file named by FAKEAGENT_PLAN:
//
//	{"<change>:<step>:<round>": action, "<change>:<step>": action,
//	 "<step>:<round>": action, "<step>": action, "*": action}
//
// The most specific key wins; with no match it reports done. Every start
// is appended to FAKEAGENT_RECORD as "<change> <step> <round> <pid>".
package main

import (
	"encoding/json"
	"fmt"
	"os"
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

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent:", err)
		os.Exit(1)
	}
}
