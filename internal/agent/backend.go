package agent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// Backend runs one step's agent: it writes the brief, starts the agent,
// waits for its result and reports the Outcome. Run documents the contract.
type Backend interface {
	// Name is the backend's name, as agent_backend spells it: "headless"
	// or "terminal".
	Name() string
	Run(ctx context.Context, s Step, opts Options) (Outcome, error)
}

// Headless is the headless backend: the agent runs as a child process of
// Tuckerbox, with its output in the step's log. It is [Run].
type Headless struct{}

// Name returns "headless".
func (Headless) Name() string { return config.BackendHeadless }

// Run runs the step headless; see [Run].
func (Headless) Run(ctx context.Context, s Step, opts Options) (Outcome, error) {
	return Run(ctx, s, opts)
}

// Probe is how [Choose] looks for herdr-core. Tests replace it.
type Probe struct {
	// LookPath finds a program on the PATH, as exec.LookPath does.
	LookPath func(name string) (string, error)
	// Output runs path with args and returns its combined output.
	Output func(ctx context.Context, path string, args ...string) ([]byte, error)
	// Timeout bounds `hc server status` (default 10 seconds).
	Timeout time.Duration
}

// SystemProbe looks on the real PATH and runs the real hc.
func SystemProbe() Probe {
	return Probe{
		LookPath: exec.LookPath,
		Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, path, args...).CombinedOutput()
		},
	}
}

// Choice is the backend chosen for this machine, and why.
type Choice struct {
	// Backend is config.BackendTerminal or config.BackendHeadless.
	Backend string
	// Reason says why, for doctor: "herdr-core wasn't found (no hc on the
	// PATH)", "agent_backend is \"headless\" in machine config", ...
	Reason string
}

// HerdrCore is the herdr-core command-line program.
const HerdrCore = "hc"

// Choose picks the agent backend for the machine config value setting
// ("" counts as auto). auto means terminal when hc is on the PATH and
// `hc server status` succeeds, else headless. terminal without a working
// hc is an error that says why; a run must then refuse to start.
func Choose(setting string, p Probe) (Choice, error) {
	if setting == "" {
		setting = config.BackendAuto
	}
	if err := config.CheckBackend(setting); err != nil {
		return Choice{}, err
	}
	if setting == config.BackendHeadless {
		return Choice{config.BackendHeadless, `agent_backend is "headless" in machine config`}, nil
	}
	why := herdrCoreProblem(p)
	switch {
	case setting == config.BackendTerminal && why != "":
		return Choice{}, fmt.Errorf(`agent_backend is "terminal" in machine config, but %s`, why)
	case setting == config.BackendTerminal:
		return Choice{config.BackendTerminal, `agent_backend is "terminal" in machine config`}, nil
	case why != "":
		return Choice{config.BackendHeadless, why}, nil
	}
	return Choice{config.BackendTerminal, "herdr-core is running"}, nil
}

// herdrCoreProblem returns why herdr-core can't run agents, or "" when it
// can.
func herdrCoreProblem(p Probe) string {
	if p.LookPath == nil || p.Output == nil {
		sys := SystemProbe()
		if p.LookPath == nil {
			p.LookPath = sys.LookPath
		}
		if p.Output == nil {
			p.Output = sys.Output
		}
	}
	path, err := p.LookPath(HerdrCore)
	if err != nil {
		return "herdr-core wasn't found (no " + HerdrCore + " on the PATH)"
	}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := p.Output(ctx, path, "server", "status")
	if err != nil {
		msg := err.Error()
		if s := oneLine(strings.TrimSpace(string(out))); s != "" {
			msg += ": " + s
		}
		return "herdr-core isn't running (" + HerdrCore + " server status: " + msg + ")"
	}
	return ""
}
