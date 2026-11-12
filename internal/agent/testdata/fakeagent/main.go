// Command fakeagent stands in for an agent CLI in tests. Its first argument
// is a mode, unless FAKEAGENT_MODE sets it (so it can run with a built-in
// agent's arguments); it reads the step from the TUCKERBOX_* environment.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	mode, args := os.Getenv("FAKEAGENT_MODE"), []string{}
	if mode == "" {
		if len(os.Args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: fakeagent <mode> [args...]")
			os.Exit(2)
		}
		mode, args = os.Args[1], os.Args[2:]
	} else {
		args = os.Args[1:]
	}
	resultPath := os.Getenv("TUCKERBOX_RESULT")
	change := os.Getenv("TUCKERBOX_CHANGE")
	step := os.Getenv("TUCKERBOX_STEP")
	round, _ := strconv.Atoi(os.Getenv("TUCKERBOX_ROUND"))
	write := func(r map[string]any) {
		data, _ := json.Marshal(r)
		tmp := resultPath + ".tmp"
		must(os.WriteFile(tmp, data, 0o644))
		// On Windows the rename fails while the engine has the file open; retry.
		var err error
		for i := 0; i < 50; i++ {
			if err = os.Rename(tmp, resultPath); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		must(err)
	}
	result := func(round int, status string) map[string]any {
		return map[string]any{"change": change, "step": step, "round": round, "status": status, "summary": "fake " + mode}
	}
	asJSON := func(r map[string]any) string {
		data, _ := json.MarshalIndent(r, "", "  ")
		return string(data)
	}
	event := func(ev map[string]any) {
		data, _ := json.Marshal(ev)
		fmt.Println(string(data))
	}
	fmt.Println("fakeagent: mode", mode)
	switch mode {
	case "message":
		// Plain text output ending with the bare result object.
		fmt.Println("I did the work.")
		fmt.Println(asJSON(result(round, "done")))
	case "message-fenced":
		r := result(round, "rejected")
		r["findings"] = []map[string]string{{"severity": "high", "file": "login.go", "detail": "password is logged"}}
		fmt.Printf("Review finished. Example of a finding: {\"severity\": \"low\"}.\n\n```json\n%s\n```\n", asJSON(r))
	case "message-malformed":
		fmt.Println("All done, the status is done. No JSON here.")
	case "message-stale":
		fmt.Println(asJSON(result(round-1, "done")))
	case "message-wrong-step":
		r := result(round, "done")
		r["step"] = "other"
		fmt.Println(asJSON(r))
	case "stream-json":
		// Claude Code's stream-json: events, then a result event whose
		// result field is the final message.
		event(map[string]any{"type": "system", "subtype": "init"})
		event(map[string]any{"type": "assistant", "message": map[string]any{"content": "{\"change\": \"decoy\"}"}})
		event(map[string]any{"type": "result", "subtype": "success", "is_error": false,
			"result": "Done.\n\n```json\n" + asJSON(result(round, "done")) + "\n```"})
	case "codex-jsonl":
		// codex exec --json: the last agent_message item is the final message.
		event(map[string]any{"type": "thread.started"})
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": "Looking."}})
		event(map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": asJSON(result(round, "done"))}})
		event(map[string]any{"type": "turn.completed"})
	case "lastmessage":
		// codex -o <path>: the CLI writes the final message to the file;
		// stdout holds nothing usable.
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "lastmessage needs a path")
			os.Exit(2)
		}
		event(map[string]any{"type": "thread.started"})
		r := result(round, "done")
		r["summary"] = "from the last-message file"
		must(os.WriteFile(args[0], []byte(asJSON(r)+"\n"), 0o644))
	case "both-same":
		write(result(round, "done"))
		fmt.Println(asJSON(result(round, "done")))
	case "both-differ":
		write(result(round, "done"))
		r := result(round, "rejected")
		r["summary"] = "the message disagrees"
		fmt.Println(asJSON(r))
	case "done":
		write(result(round, "done"))
	case "rejected":
		r := result(round, "rejected")
		r["findings"] = []map[string]string{{"severity": "high", "file": "login.go", "detail": "password is logged"}}
		write(r)
	case "argv":
		// Record what we were started with, as the result's summary.
		wd, _ := os.Getwd()
		info, _ := json.Marshal(map[string]any{"args": args, "wd": wd})
		r := result(round, "done")
		r["summary"] = string(info)
		write(r)
	case "report":
		// Report the arguments, with long ones as their length and hash, and
		// what came on standard input, in the final message. It opens no
		// file: it reads neither the brief file nor the state folder.
		in, err := io.ReadAll(os.Stdin)
		must(err)
		short := make([]string, len(args))
		for i, a := range args {
			short[i] = digest(a)
		}
		info, _ := json.Marshal(map[string]any{"args": short, "stdin": digest(string(in))})
		r := result(round, "done")
		r["summary"] = string(info)
		// Like codex -o, write the final message to the -o file if given.
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "-o" {
				must(os.WriteFile(args[i+1], []byte(asJSON(r)+"\n"), 0o644))
				return
			}
		}
		fmt.Println(asJSON(r))
	case "stale-then-done":
		write(result(round-1, "done"))
		time.Sleep(700 * time.Millisecond)
		write(result(round, "done"))
	case "malformed-then-done":
		must(os.WriteFile(resultPath, []byte(`{"change": "`), 0o644))
		time.Sleep(700 * time.Millisecond)
		write(result(round, "done"))
	case "stale-only":
		write(result(round-1, "done"))
		time.Sleep(300 * time.Millisecond)
	case "noresult":
		fmt.Println("something went wrong")
		fmt.Fprintln(os.Stderr, "fatal: boom")
		os.Exit(3)
	case "slow":
		for i := 1; i <= 10; i++ {
			fmt.Printf("line %d\n", i)
			time.Sleep(100 * time.Millisecond)
		}
		write(result(round, "done"))
	case "linger":
		write(result(round, "done"))
		time.Sleep(time.Minute)
	case "hang":
		time.Sleep(time.Minute)
	case "elsewhere":
		// Writes a valid-looking result to another path only.
		data, _ := json.Marshal(result(round, "done"))
		must(os.WriteFile(filepath.Join(filepath.Dir(resultPath), "other.json"), data, 0o644))
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", mode)
		os.Exit(2)
	}
}

// digest is s itself when short, else its length in bytes and SHA-256.
func digest(s string) string {
	if len(s) <= 200 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%d bytes sha256:%s", len(s), hex.EncodeToString(sum[:]))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
