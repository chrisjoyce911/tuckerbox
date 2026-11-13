package machines

import (
	"encoding/json"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
)

// ReportVersion is the format version of [Report].
const ReportVersion = 1

// Sign-in states of an agent CLI, as package doctor reports them.
const (
	SignedIn  = doctor.SignedIn
	SignedOut = doctor.SignedOut
	Unknown   = doctor.Unknown
)

// Report is what tuckerbox machine --json prints: what a machine says
// about itself. It never holds a token or a permission flag.
type Report struct {
	// Format is the report's format version, [ReportVersion].
	Format int `json:"format"`
	// Name is the machine's name: machine config `machine`, else the host
	// name.
	Name string `json:"name"`
	// Version is the Tuckerbox version running there.
	Version string `json:"version"`
	// OS is the operating system, as runtime.GOOS.
	OS string `json:"os"`
	// FleetCapacity is machine config fleet_capacity; FleetRuns is how
	// many fleet slots are held now.
	FleetCapacity int `json:"fleet_capacity"`
	FleetRuns     int `json:"fleet_runs"`
	// Agents lists every built-in and configured agent CLI.
	Agents []AgentReport `json:"agents"`
	// For is the remote asked about with --for, normalised; "" without it.
	For string `json:"for,omitempty"`
	// Checkout says whether this machine has a checkout of For, and Path
	// where it is. Only with --for.
	Checkout bool   `json:"checkout"`
	Path     string `json:"path,omitempty"`
	// Runs are this machine's runs of For. Only with --for.
	Runs []RunReport `json:"runs"`
}

// Free is the machine's free fleet capacity.
func (r Report) Free() int {
	if n := r.FleetCapacity - r.FleetRuns; n > 0 {
		return n
	}
	return 0
}

// Agent returns the named agent's report.
func (r Report) Agent(name string) (AgentReport, bool) {
	for _, a := range r.Agents {
		if a.Name == name {
			return a, true
		}
	}
	return AgentReport{}, false
}

// AgentReport is one agent CLI on a machine.
type AgentReport struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	// SignedIn is true or false when known, null when unknown.
	SignedIn *bool `json:"signed_in"`
	// SignIn is SignedIn as text: "signed in", "signed out" or "unknown".
	SignIn string `json:"sign_in"`
}

// SetSignIn sets SignIn and SignedIn from one of the sign-in states.
func (a *AgentReport) SetSignIn(state string) {
	a.SignIn = state
	switch state {
	case SignedIn:
		t := true
		a.SignedIn = &t
	case SignedOut:
		f := false
		a.SignedIn = &f
	default:
		a.SignIn = Unknown
		a.SignedIn = nil
	}
}

// RunReport is one run of the asked-about repository on a machine.
type RunReport struct {
	Change string `json:"change"`
	State  string `json:"state"`
	// Since is when the run entered State, RFC 3339 with nanoseconds, as
	// run.json holds it.
	Since string `json:"since"`
	// Scope is the run's task scope, "" for the whole change.
	Scope      string `json:"scope"`
	Complete   bool   `json:"complete"`
	Dispatched bool   `json:"dispatched"`
}

// ParseReport reads a machine report.
func ParseReport(data []byte) (Report, error) {
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return Report{}, fmt.Errorf("reading the machine report: %w", err)
	}
	if r.Format != ReportVersion {
		return Report{}, fmt.Errorf("machine report format %d; this Tuckerbox reads format %d (update Tuckerbox on %s)", r.Format, ReportVersion, r.Name)
	}
	return r, nil
}
