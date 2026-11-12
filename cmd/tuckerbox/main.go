// Command tuckerbox runs OpenSpec changes through coding agents.
package main

import (
	"os"

	"github.com/chrisjoyce911/tuckerbox/internal/cli"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
)

func main() {
	app := cli.App{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		DoctorEnv:   doctor.SystemEnv(),
		DoctorTools: doctor.DefaultTools,
	}
	os.Exit(app.Run(os.Args[1:]))
}
