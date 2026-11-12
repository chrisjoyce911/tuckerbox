// A cross-platform SSH stand-in for process cancellation tests.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	switch os.Getenv("FAKE_SSH_MODE") {
	case "fail":
		os.Exit(1)
	case "slow":
		time.Sleep(10 * time.Second)
	case "tree":
		self, _ := os.Executable()
		c := exec.Command(self, "descendant")
		c.Env = append(os.Environ(), "FAKE_SSH_MODE=descendant")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Start(); err != nil {
			panic(err)
		}
		os.WriteFile(os.Getenv("FAKE_SSH_STARTED"), []byte("ready"), 0600)
		time.Sleep(10 * time.Second)
	case "descendant":
		time.Sleep(time.Second)
		os.WriteFile(os.Getenv("FAKE_SSH_SURVIVED"), []byte("still alive"), 0600)
	default:
		fmt.Println("hostname github.com")
	}
}
