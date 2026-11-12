// Command fakehc stands in for herdr-core's hc in tests. It supports:
//
//	hc server status   succeeds, printing a status object, when
//	                   FAKEHC_SERVER is "up"; otherwise fails, saying the
//	                   server is not running
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := strings.Join(os.Args[1:], " ")
	switch args {
	case "server status":
		if os.Getenv("FAKEHC_SERVER") == "up" {
			fmt.Println(`{"core":{"name":"fake"},"live":0}`)
			return
		}
		fmt.Fprintln(os.Stderr, "herdr-core server is not running")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "fakehc: unsupported command %q\n", args)
		os.Exit(2)
	}
}
