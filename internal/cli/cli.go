// Package cli parses the tuckerbox command line and runs its commands.
package cli

import (
	"fmt"
	"io"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

const usage = `Tuckerbox runs OpenSpec changes through coding agents.

Usage:
  tuckerbox <command> [arguments]

Commands:
  run <change>... | --all   Run OpenSpec changes through the workflow, one
                            background process per change (--foreground:
                            one change, attached to this terminal)
  board [--json] [--change <id>]  Show this repository’s change board
  status [--json]           Show every run on this machine
  logs <change> [step]      Show a step's agent output (--follow to watch)
  stop <change>             Stop a run's agent and mark the run stopped
  resume <change>           Continue a stopped or blocked run
  rerun <change> <step>     Start a run again from a step
  history <change> [--json] Show a run's events, oldest first
  edit <change> <file>      Open proposal, design, model, tasks or
                            spec:<capability> in your editor, then validate
  validate <change> [--json]  Validate a change strictly
  tasks <change> tick|untick <n.m>  Tick or untick one task in tasks.md
  approve <change>          Approve a change's text so runs may start it
  unapprove <change>        Withdraw a change's approval
  version [--json]          Print the version, commit, Go version and platform
  doctor                    Check that git, openspec and the agent CLIs are installed
  check                     Validate .tuckerbox/config.toml with machine config
  init                      Write a commented starter .tuckerbox/config.toml
  help                      Show this help
`

// App holds what commands need from the outside world, so tests can replace it.
type App struct {
	Stdout, Stderr io.Writer
	DoctorEnv      doctor.Env
	DoctorTools    []doctor.Tool
	// ConfigEnv and Getwd are used by check and init; zero values use the
	// real git, user config directory and working directory.
	ConfigEnv config.Env
	Getwd     func() (string, error)
	// Executable is the tuckerbox binary that detached runs re-execute;
	// nil means os.Executable.
	Executable func() (string, error)
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
		return a.version(cmd, rest)
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
	case "check":
		if len(rest) > 0 {
			return a.tooMany(cmd)
		}
		return a.check()
	case "init":
		if len(rest) > 0 {
			return a.tooMany(cmd)
		}
		return a.initConfig()
	case "run":
		return a.run(rest)
	case "engine":
		return a.engine(rest)
	case "status":
		return a.status(rest)
	case "logs":
		return a.logs(rest)
	case "stop":
		return a.stop(rest)
	case "resume":
		return a.resume(rest)
	case "rerun":
		return a.rerun(rest)
	case "board":
		return a.board(rest)
	case "history":
		return a.history(rest)
	case "edit":
		return a.edit(rest)
	case "validate":
		return a.validate(rest)
	case "tasks":
		return a.tasks(rest)
	case "approve":
		return a.approve(rest)
	case "unapprove":
		return a.unapprove(rest)
	default:
		fmt.Fprintf(a.Stderr, "tuckerbox: unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// version prints the version information as text, or as JSON with --json.
// Any other argument is a usage error.
func (a App) version(cmd string, args []string) int {
	asJSON := false
	for _, arg := range args {
		if arg != "--json" || asJSON {
			fmt.Fprintf(a.Stderr, "tuckerbox %s: unknown argument %q (takes no arguments except --json)\n", cmd, arg)
			return 2
		}
		asJSON = true
	}
	info := version.Get()
	if !asJSON {
		fmt.Fprint(a.Stdout, info.String())
		return 0
	}
	data, err := info.JSON()
	if err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox:", err)
		return 1
	}
	a.Stdout.Write(data)
	return 0
}

func (a App) tooMany(cmd string) int {
	fmt.Fprintf(a.Stderr, "tuckerbox %s: takes no arguments\n", cmd)
	return 2
}
