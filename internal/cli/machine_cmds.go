package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/doctor"
	"github.com/chrisjoyce911/tuckerbox/internal/fleetslot"
	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
	"github.com/chrisjoyce911/tuckerbox/internal/machines"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/version"
)

// machineConfig loads this machine's config alone, for commands that need
// no repository: the machine's name, fleet_capacity, [machines] and
// custom agents.
func (a App) machineConfig() (*config.Config, error) {
	env, _ := a.configEnv()
	path, err := config.MachinePath(env)
	if err != nil {
		return nil, err
	}
	return config.LoadMachine(path)
}

// machineName is this machine's name: machine config `machine`, else the
// host name.
func machineName(cfg *config.Config) string {
	configured := ""
	if cfg != nil {
		configured = cfg.Machine
	}
	name, err := ship.MachineName(configured)
	if err != nil || name == "" {
		return "this machine"
	}
	return name
}

// customAgents maps each agent defined in config, other than the built-ins,
// to its command's program.
func customAgents(cfg *config.Config) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	for name, ag := range cfg.Agents {
		if config.IsBuiltinAgent(name) || len(ag.Command) == 0 {
			continue
		}
		out[name] = ag.Command[0]
	}
	return out
}

// doctorTools is what doctor checks: the app's tools, and each custom agent
// of the repository (when doctor runs in one) or of machine config.
func (a App) doctorTools() []doctor.Tool {
	tools := slices.Clone(a.DoctorTools)
	cfg := a.doctorConfig()
	for _, t := range doctor.AgentTools(customAgents(cfg)) {
		if !slices.ContainsFunc(tools, func(x doctor.Tool) bool { return x.Name == t.Name }) {
			tools = append(tools, t)
		}
	}
	return tools
}

func (a App) doctorConfig() *config.Config {
	env, getwd := a.configEnv()
	if wd, err := getwd(); err == nil {
		if opts, err := config.Discover(env, wd); err == nil {
			if cfg, err := config.Load(opts); err == nil {
				return cfg
			}
		}
	}
	cfg, err := a.machineConfig()
	if err != nil {
		return nil
	}
	return cfg
}

// machine: tuckerbox machine --json [--for <remote>]
func (a App) machine(args []string) int {
	var asJSON bool
	var forRemote string
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, map[string]*string{"for": &forRemote})
	if err != nil || len(pos) > 0 {
		return a.usageErr("machine", "usage: tuckerbox machine [--json] [--for <remote>]")
	}
	cfg, err := a.machineConfig()
	if err != nil {
		return a.errf("machine", "%v", err)
	}
	rep, err := a.machineReport(context.Background(), cfg, forRemote)
	if err != nil {
		return a.errf("machine", "%v", err)
	}
	if asJSON {
		return a.writeJSON(rep)
	}
	fmt.Fprintf(a.Stdout, "%s: tuckerbox %s on %s, fleet runs %d of %d\n", rep.Name, rep.Version, rep.OS, rep.FleetRuns, rep.FleetCapacity)
	rows := [][]string{{"AGENT", "INSTALLED", "SIGN-IN"}}
	for _, ag := range rep.Agents {
		rows = append(rows, []string{ag.Name, strconv.FormatBool(ag.Installed), ag.SignIn})
	}
	writeTable(a.Stdout, rows)
	if forRemote != "" {
		if !rep.Checkout {
			fmt.Fprintf(a.Stdout, "no checkout of %s\n", rep.For)
		} else {
			fmt.Fprintf(a.Stdout, "checkout of %s: %s, %d run(s)\n", rep.For, rep.Path, len(rep.Runs))
		}
	}
	return 0
}

// machineReport is what this machine says about itself (machine-inventory:
// Machine report). It holds no token or permission flag.
func (a App) machineReport(ctx context.Context, cfg *config.Config, forRemote string) (rep machines.Report, err error) {
	info := version.Get()
	rep = machines.Report{
		Format: machines.ReportVersion, Name: machineName(cfg), Version: info.Version, OS: info.OS,
		FleetCapacity: cfg.FleetCapacity, Agents: []machines.AgentReport{}, Runs: []machines.RunReport{},
	}
	home, err := a.stateHome()
	if err != nil {
		return rep, err
	}
	if rep.FleetRuns, err = fleetslot.InUse(home, cfg.FleetCapacity); err != nil {
		return rep, err
	}
	agents := customAgents(cfg)
	defer func() {
		for _, r := range doctor.Check(a.DoctorEnv, doctor.AgentTools(agents)) {
			ag := machines.AgentReport{Name: r.Tool.Name, Installed: r.Found}
			if r.Found {
				ag.SetSignIn(r.SignIn)
			} else {
				ag.SetSignIn(machines.Unknown)
			}
			rep.Agents = append(rep.Agents, ag)
		}
	}()
	if forRemote == "" {
		return rep, nil
	}
	env, _ := a.configEnv()
	key := gitremote.Key(ctx, forRemote)
	if key == "" {
		key = forRemote
	}
	rep.For = key
	checkout, err := board.Find(ctx, key, "", home, env)
	if err != nil {
		return rep, nil // no checkout: Checkout stays false
	}
	rep.Checkout, rep.Path = true, checkout
	// Agents the repository's own config defines count too: a step may
	// name one.
	if opts, err := config.Discover(env, checkout); err == nil {
		if repoCfg, err := config.Load(opts); repoCfg != nil && (err == nil || errors.As(err, new(config.Problems))) {
			for name, prog := range customAgents(repoCfg) {
				agents[name] = prog
			}
		}
	}
	ix, err := runs.ReadIndex(home)
	if err != nil {
		return rep, err
	}
	for _, e := range ix.Runs {
		if !sameDir(e.Repo, checkout) {
			continue
		}
		r, err := runs.ReadRun(statedir.Dir(e.State).RunFile())
		if err != nil {
			continue
		}
		rr := machines.RunReport{Change: r.Change, State: string(r.State), Complete: r.Complete, Dispatched: r.Dispatched}
		if !r.Since.IsZero() {
			rr.Since = r.Since.Format(time.RFC3339Nano)
		}
		if r.Scope != nil {
			rr.Scope = r.Scope.Spec
		}
		rep.Runs = append(rep.Runs, rr)
	}
	slices.SortFunc(rep.Runs, func(x, y machines.RunReport) int { return strings.Compare(x.Change, y.Change) })
	return rep, nil
}

func sameDir(a, b string) bool {
	ca, err1 := filepath.EvalSymlinks(a)
	cb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ca == cb
}

// remoteResult is one machine's answer to a command run there.
type remoteResult struct {
	Machine string
	Out     []byte
	Err     error
}

// onMachines runs tuckerbox args on every listed machine at once, with
// timeout, and returns the answers by machine name, sorted.
func (a App) onMachines(ctx context.Context, cfg *config.Config, args []string, timeout time.Duration) []remoteResult {
	names := slices.Sorted(maps.Keys(cfg.Machines))
	out := make([]remoteResult, len(names))
	// Never pass on a configured forge token variable.
	runner := a.MachineRunner.ForConfig(cfg)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := runner.Run(ctx, cfg.Machines[name], args, timeout)
			out[i] = remoteResult{Machine: name, Out: b, Err: err}
		}()
	}
	wg.Wait()
	return out
}

// statusAllMachines: tuckerbox status --all-machines [--json]
func (a App) statusAllMachines(asJSON bool) int {
	cfg, err := a.machineConfig()
	if err != nil {
		return a.errf("status", "%v", err)
	}
	local, err := a.statusRuns()
	if err != nil {
		return a.errf("status", "%v", err)
	}
	here := machineName(cfg)
	for i := range local {
		local[i].Machine = here
	}
	list := local
	unreachable := []string{}
	for _, res := range a.onMachines(context.Background(), cfg, []string{"status", "--json"}, machines.DefaultReportTimeout) {
		var doc struct {
			Version int         `json:"version"`
			Runs    []statusRun `json:"runs"`
		}
		err := res.Err
		if err == nil {
			err = json.Unmarshal(res.Out, &doc)
		}
		if err == nil && doc.Version != StatusVersion {
			err = fmt.Errorf("status format %d; update Tuckerbox there", doc.Version)
		}
		if err != nil {
			unreachable = append(unreachable, res.Machine)
			if errors.Is(err, machines.ErrUnreachable) {
				fmt.Fprintf(a.Stderr, "tuckerbox status: %s is unreachable: %v\n", res.Machine, err)
			} else {
				fmt.Fprintf(a.Stderr, "tuckerbox status: %s: %v\n", res.Machine, err)
			}
			continue
		}
		for _, r := range doc.Runs {
			r.Machine = res.Machine
			list = append(list, r)
		}
	}
	if asJSON {
		return a.writeJSON(map[string]any{"version": StatusVersion, "runs": list, "unreachable": unreachable})
	}
	if len(list) == 0 {
		fmt.Fprintln(a.Stdout, "no runs on any machine")
		return 0
	}
	rows := [][]string{{"MACHINE", "REPO", "CHANGE", "STATE", "STEP", "TASKS", "SINCE", "REASON"}}
	for _, r := range list {
		since, _ := time.Parse(time.RFC3339, r.Since)
		step := r.Step
		if step == "" {
			step = "-"
		}
		tasks := "all"
		if r.Scope != nil {
			tasks = r.Scope.Spec
		}
		rows = append(rows, []string{r.Machine, filepath.Base(r.Repo), r.Change, r.State, step, tasks, displayTime(since), r.Reason})
	}
	writeTable(a.Stdout, rows)
	return 0
}
