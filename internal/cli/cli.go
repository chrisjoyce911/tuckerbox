// Package cli parses the tuckerbox command line and runs its commands.
package cli

import (
	"fmt"
	"io"

	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

const usage = `Tuckerbox runs OpenSpec changes through coding agents.

Usage:
  tuckerbox <command>

Commands:
  version   Print the version, commit, Go version and platform
  doctor    Check that git, openspec and the agent CLIs are installed
  help      Show this help
`

// App holds what commands need from the outside world, so tests can replace it.
type App struct {
	Stdout, Stderr io.Writer
	DoctorEnv      doctor.Env
	DoctorTools    []doctor.Tool
}

// Run runs the command in args (without the program name) and returns the
// process exit code.
func (a App) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(a.Stdout, usage)
		return 0
	case "version", "--version":
		if len(rest) > 0 {
			return a.tooMany(cmd)
		}
		fmt.Fprint(a.Stdout, version.Get().String())
		return 0
	case "doctor":
		if len(rest) > 0 {
			return a.tooMany(cmd)
		}
		results := doctor.Check(a.DoctorEnv, a.DoctorTools)
		if err := doctor.Write(a.Stdout, results); err != nil {
			fmt.Fprintln(a.Stderr, "tuckerbox:", err)
			return 1
		}
		if !doctor.OK(results) {
			return 1
		}
		return 0
	default:
		fmt.Fprintf(a.Stderr, "tuckerbox: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

func (a App) tooMany(cmd string) int {
	fmt.Fprintf(a.Stderr, "tuckerbox %s: takes no arguments\n", cmd)
	return 2
}
