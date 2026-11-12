package cli

import (
	"fmt"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// chooseBackend picks the agent backend for the machine config value
// setting, looking for herdr-core the way doctor looks for tools.
func (a App) chooseBackend(setting string) (agent.Choice, error) {
	return agent.Choose(setting, agent.Probe{
		LookPath: a.DoctorEnv.LookPath,
		Output:   a.DoctorEnv.Output,
		Timeout:  a.DoctorEnv.Timeout,
	})
}

// backendDoctorLine is doctor's line about the agent backend: which one
// runs agents on this machine, and why. It never fails doctor; a run
// refuses by itself when the backend can't be used.
func (a App) backendDoctorLine() string {
	env, _ := a.configEnv()
	path, err := config.MachinePath(env)
	if err != nil {
		return "agent backend: can't read machine config (" + err.Error() + ")"
	}
	setting, err := config.MachineBackend(path)
	if err != nil {
		return "agent backend: can't read machine config (" + err.Error() + ")"
	}
	c, err := a.chooseBackend(setting)
	if err != nil {
		return "agent backend: none, runs refuse to start: " + err.Error()
	}
	return fmt.Sprintf("agent backend: %s, because %s", c.Backend, c.Reason)
}
