// Command fakeopenspec stands in for the openspec CLI in tests. It
// supports the two commands Tuckerbox uses:
//
//	openspec list --json      lists openspec/changes/* except archive
//	openspec validate <id> …  fails when the change does not exist or holds
//	                          a file named INVALID (whose text it prints)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: openspec list --json | validate <id>")
		os.Exit(2)
	}
	changes := filepath.Join("openspec", "changes")
	switch os.Args[1] {
	case "list":
		entries, _ := os.ReadDir(changes)
		type item struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		var items []item
		for _, e := range entries {
			if e.IsDir() && e.Name() != "archive" {
				items = append(items, item{Name: e.Name(), Status: "in-progress"})
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name > items[j].Name })
		data, _ := json.MarshalIndent(map[string]any{"changes": items}, "", "  ")
		fmt.Println(string(data))
	case "validate":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "validate needs an id")
			os.Exit(2)
		}
		id := os.Args[2]
		dir := filepath.Join(changes, id)
		if _, err := os.Stat(filepath.Join(dir, "proposal.md")); err != nil {
			fmt.Fprintf(os.Stderr, "Unknown item '%s'\n", id)
			os.Exit(1)
		}
		if msg, err := os.ReadFile(filepath.Join(dir, "INVALID")); err == nil {
			fmt.Printf("Change '%s' has issues\n%s", id, msg)
			os.Exit(1)
		}
		fmt.Printf("Change '%s' is valid\n", id)
	default:
		fmt.Fprintln(os.Stderr, "fakeopenspec: unsupported command", os.Args[1])
		os.Exit(2)
	}
}
