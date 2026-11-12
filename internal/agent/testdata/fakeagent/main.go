// Command fakeagent stands in for an agent CLI in tests. Its first argument
// is a mode; it reads the step from the TUCKERBOX_* environment.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeagent <mode> [args...]")
		os.Exit(2)
	}
	mode := os.Args[1]
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
	fmt.Println("fakeagent: mode", mode)
	switch mode {
	case "done":
		write(result(round, "done"))
	case "rejected":
		r := result(round, "rejected")
		r["findings"] = []map[string]string{{"severity": "high", "file": "login.go", "detail": "password is logged"}}
		write(r)
	case "argv":
		// Record what we were started with, as the result's summary.
		wd, _ := os.Getwd()
		info, _ := json.Marshal(map[string]any{"args": os.Args[2:], "wd": wd})
		r := result(round, "done")
		r["summary"] = string(info)
		write(r)
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

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
