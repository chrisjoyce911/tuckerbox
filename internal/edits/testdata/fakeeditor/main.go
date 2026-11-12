// Command fakeeditor stands in for a person's editor in tests. The file to
// edit is its last argument; flags before it say what to do:
//
//	--opened <path>    write the edited file's path to <path> once started
//	--wait-for <path>  wait until <path> exists before closing
//	--append <text>    append text to the file
//	--invalid <text>   write <text> to INVALID in the change folder (the
//	                   nearest folder up with a proposal.md), which the
//	                   openspec stub reports as a validation error
//	--exit <n>         exit with status n
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fakeeditor: no file")
		os.Exit(2)
	}
	file := args[len(args)-1]
	flags := map[string]string{}
	for i := 0; i+1 < len(args)-1; i += 2 {
		flags[args[i]] = args[i+1]
	}
	if p, ok := flags["--opened"]; ok {
		must(os.WriteFile(p+".tmp", []byte(file), 0o644))
		must(os.Rename(p+".tmp", p))
	}
	if p, ok := flags["--wait-for"]; ok {
		deadline := time.Now().Add(60 * time.Second)
		for {
			if _, err := os.Stat(p); err == nil {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "fakeeditor: timed out waiting for", p)
				os.Exit(3)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if t, ok := flags["--append"]; ok {
		f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		must(err)
		_, err = f.WriteString(t)
		must(err)
		must(f.Close())
	}
	if t, ok := flags["--invalid"]; ok {
		dir := filepath.Dir(file)
		for i := 0; i < 4; i++ {
			if _, err := os.Stat(filepath.Join(dir, "proposal.md")); err == nil {
				break
			}
			dir = filepath.Dir(dir)
		}
		must(os.WriteFile(filepath.Join(dir, "INVALID"), []byte(t+"\n"), 0o644))
	}
	if n, ok := flags["--exit"]; ok {
		code, _ := strconv.Atoi(n)
		os.Exit(code)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeeditor:", err)
		os.Exit(1)
	}
}
