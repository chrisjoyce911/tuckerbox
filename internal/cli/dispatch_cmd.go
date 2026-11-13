package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/dispatch"
	"github.com/chrisjoyce911/tuckerbox/internal/machines"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// dispatchStartTimeout bounds one remote start: the started run answers
// once it holds its change lock, as tuckerbox run does locally.
var dispatchStartTimeout = 2 * time.Minute

// dispatchResult is tuckerbox dispatch --json.
type dispatchResult struct {
	Remote string `json:"remote"`
	DryRun bool   `json:"dry_run"`
	dispatch.Plan
	// Started and Failed are the actions' outcomes (empty on a dry run).
	Started []dispatch.Action `json:"started"`
	Failed  []dispatchFailure `json:"failed"`
}

type dispatchFailure struct {
	dispatch.Action
	Error string `json:"error"`
}

// dispatch: tuckerbox dispatch [change...] [--dry-run] [--json]
func (a App) dispatch(args []string) int {
	var dry, asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"dry-run": &dry, "json": &asJSON}, nil)
	if err != nil {
		return a.usageErr("dispatch", "usage: tuckerbox dispatch [<change>...] [--dry-run] [--json]")
	}
	for _, id := range pos {
		if err := statedir.CheckID("change", id); err != nil {
			return a.errf("dispatch", "%v", err)
		}
	}
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("dispatch", "%v", err)
	}
	repo, cfg, ok := a.loadRepo("dispatch", wd)
	if !ok {
		return 1
	}
	switch cfg.Mode {
	case config.ModeFleet:
	case config.ModeTeam:
		return a.errf("dispatch", "this is a team repository (mode = \"team\"): each person starts their own runs with tuckerbox run; nothing started")
	default:
		return a.errf("dispatch", "this repository is solo (mode is not \"fleet\"): dispatch places changes only in fleet mode; nothing started")
	}
	ctx := context.Background()
	remote := board.RepoKey(ctx, config.Remote(env, repo.Main))
	if remote == "" {
		return a.errf("dispatch", "fleet mode needs a remote; nothing started")
	}
	in, err := a.dispatchInput(ctx, repo, cfg, remote, pos)
	if err != nil {
		return a.errf("dispatch", "%v", err)
	}
	plan := dispatch.Decide(in)
	res := dispatchResult{Remote: remote, DryRun: dry, Plan: plan, Started: []dispatch.Action{}, Failed: []dispatchFailure{}}
	if !dry {
		byName := map[string]dispatch.Machine{}
		for _, m := range in.Machines {
			byName[m.Name] = m
		}
		// Placement is advice: a refused claim or a failed start is
		// reported and not retried elsewhere in this pass.
		for _, act := range plan.Actions {
			out, err := a.dispatchOn(ctx, repo, cfg, byName[act.Machine], act.Args(remote), dispatchStartTimeout)
			if err != nil {
				res.Failed = append(res.Failed, dispatchFailure{Action: act, Error: startError(out, err)})
				continue
			}
			res.Started = append(res.Started, act)
		}
	}
	if asJSON {
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			return a.errf("dispatch", "%v", err)
		}
		return 0
	}
	a.printDispatch(res)
	return 0
}

func (a App) printDispatch(res dispatchResult) {
	w := a.Stdout
	for _, u := range res.Unreachable {
		fmt.Fprintln(w, u)
	}
	verb := func(act dispatch.Action) string {
		if act.Kind == dispatch.Continue {
			return "continue " + act.Change + " --tasks next"
		}
		return "start " + act.Change
	}
	notes := func(act dispatch.Action) string {
		if len(act.Notes) == 0 {
			return ""
		}
		return " (" + strings.Join(act.Notes, "; ") + ")"
	}
	if res.DryRun {
		for _, act := range res.Actions {
			fmt.Fprintf(w, "would %s on %s%s\n", verb(act), act.Machine, notes(act))
		}
	}
	for _, act := range res.Started {
		fmt.Fprintf(w, "%s on %s%s\n", verb(act), act.Machine, notes(act))
	}
	for _, f := range res.Failed {
		fmt.Fprintf(w, "%s on %s failed: %s\n", verb(f.Action), f.Machine, f.Error)
	}
	for _, s := range res.Skipped {
		fmt.Fprintf(w, "%s: %s\n", s.Change, s.Why)
		for _, n := range s.Notes {
			fmt.Fprintf(w, "  %s\n", n)
		}
	}
	if len(res.Actions) == 0 && len(res.Skipped) == 0 {
		fmt.Fprintln(w, "no active changes to dispatch")
	}
}

// startError is the started command's own message, else err's.
func startError(out []byte, err error) string {
	var me *machines.Error
	if errors.As(err, &me) && strings.TrimSpace(me.Stderr) != "" {
		return lastLines(me.Stderr)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return lastLines(s)
	}
	return err.Error()
}

func lastLines(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var keep []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "tuckerbox run: nothing started") {
			keep = append(keep, strings.TrimPrefix(l, "tuckerbox run: "))
		}
	}
	return strings.Join(keep, "; ")
}

// dispatchInput gathers a pass's input: the active changes at the fetched
// base in admission order, the fleet ref's claims, and every machine's
// report, read in parallel.
func (a App) dispatchInput(ctx context.Context, repo runs.Repo, cfg *config.Config, remote string, only []string) (dispatch.Input, error) {
	in := dispatch.Input{MaxRuns: cfg.Fleet.MaxRuns, GroupScope: cfg.Autopilot.Scope != config.AutopilotChange}
	gate := &runGate{a: a, repo: repo, cfg: cfg}
	base, err := repo.Base(ctx, cfg.Worktree.Base, func(msg string) {
		fmt.Fprintf(a.Stderr, "tuckerbox dispatch: warning: %s\n", msg)
	})
	if err != nil {
		return in, err
	}
	rev, err := runs.RevOf(ctx, repo, base)
	if err != nil {
		return in, err
	}
	ids, err := runs.ActiveAt(ctx, repo, rev)
	if err != nil {
		return in, err
	}
	if len(only) > 0 {
		want := map[string]bool{}
		for _, id := range only {
			want[id] = true
		}
		active := map[string]bool{}
		var keep []string
		for _, id := range ids {
			active[id] = true
			if want[id] {
				keep = append(keep, id)
			}
		}
		for _, id := range only {
			if !active[id] {
				return in, fmt.Errorf("%s is not an active change on %s", id, base)
			}
		}
		ids = keep
	}
	if ids, err = admissionOrder(repo, ids); err != nil {
		return in, err
	}
	shipping := cfg.Ship != "" && cfg.Ship != config.ShipLocal
	claims, err := fleetClaims(ctx, repo.Root, cfg.Shipping.Remote, cfg.Fleet.Lease, time.Now())
	if err != nil {
		return in, fmt.Errorf("reading the fleet ref: %v", err)
	}
	in.Claims = claims
	archived, err := runs.ArchivedAt(ctx, repo, rev)
	if err != nil {
		return in, err
	}
	allActive, err := runs.ActiveAt(ctx, repo, rev)
	if err != nil {
		return in, err
	}
	isActive := map[string]bool{}
	for _, id := range allActive {
		isActive[id] = true
	}
	ospec := runs.OpenSpec{Dir: repo.Root}
	for _, id := range ids {
		ch := dispatch.Change{ID: id}
		data, has := runs.ShowAt(ctx, repo, rev, "openspec/changes/"+id+"/"+settings.FileName)
		if has {
			sc, _, err := settings.Parse([]byte(data))
			if err == nil {
				ch.Machines = sc.Machines
				for _, st := range cfg.Steps {
					ch.Agents = append(ch.Agents, settings.Resolve(cfg, st, sc.Steps[st.ID]).Agent)
				}
			}
		}
		if ch.Agents == nil {
			for _, st := range cfg.Steps {
				ch.Agents = append(ch.Agents, st.Agent)
			}
		}
		_, err := gate.check(ctx, id)
		if err == nil {
			err = gate.overrides(ctx, id)
		}
		ch.Approved = err == nil
		var na *notApprovedError
		var oe *runs.OverridesError
		switch {
		case ospec.Validate(ctx, id) != nil:
			ch.NotStartable = "not valid (tuckerbox validate " + id + ")"
		case errors.As(err, &na):
			ch.NotStartable = "not approved (" + string(na.State) + ")"
		case errors.As(err, &oe):
			ch.NotStartable = "step overrides not allowed"
		case err != nil:
			ch.NotStartable = err.Error()
		default:
			if why := unmetDeps(ctx, repo, rev, id, archived, isActive, claims, shipping); why != "" {
				ch.NotStartable = why
			}
		}
		in.Changes = append(in.Changes, ch)
	}
	in.Machines = a.machineReports(ctx, repo, cfg, remote)
	return in, nil
}

// unmetDeps says which of change id's dependencies (its tuckerbox.toml at
// rev) are not met, or "". A dependency is met when archived at rev or,
// with ship = "local", when its fleet claim is ready.
func unmetDeps(ctx context.Context, repo runs.Repo, rev, id string, archived, active map[string]bool, claims map[string]dispatch.Claim, shipping bool) string {
	data, ok := runs.ShowAt(ctx, repo, rev, "openspec/changes/"+id+"/"+deps.FileName)
	if !ok {
		return ""
	}
	var f deps.File
	if _, err := toml.Decode(data, &f); err != nil {
		return deps.FileName + ": " + err.Error()
	}
	var unmet []string
	for _, d := range f.DependsOn {
		d = strings.TrimSpace(d)
		switch {
		case d == "" || archived[d]:
		case !active[d]:
			unmet = append(unmet, d+" (not found)")
		case !shipping && claims[d].State == dispatch.ClaimReady:
		default:
			unmet = append(unmet, d)
		}
	}
	if len(unmet) == 0 {
		return ""
	}
	return "waiting on " + strings.Join(unmet, ", ")
}

// machineReports asks this machine and every [machines.*] entry for its
// report on remote, in parallel, each within the report timeout.
func (a App) machineReports(ctx context.Context, repo runs.Repo, cfg *config.Config, remote string) []dispatch.Machine {
	here, err := ship.MachineName(cfg.Machine)
	if err != nil || here == "" {
		here = "this machine"
	}
	ms := []dispatch.Machine{{Name: here, Local: true}}
	var names []string
	for name := range cfg.Machines {
		if name != here {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		ms = append(ms, dispatch.Machine{Name: n})
	}
	var wg sync.WaitGroup
	for i := range ms {
		wg.Add(1)
		go func(m *dispatch.Machine) {
			defer wg.Done()
			out, err := a.dispatchOn(ctx, repo, cfg, *m, []string{"machine", "--json", "--for", remote}, machines.DefaultReportTimeout)
			if err != nil {
				m.Err = errors.New(startError(out, err))
				return
			}
			var r dispatch.Report
			if err := json.Unmarshal(out, &r); err != nil {
				m.Err = fmt.Errorf("its report is not JSON: %v", err)
				return
			}
			m.Report = &r
		}(&ms[i])
	}
	wg.Wait()
	return ms
}

// dispatchOn runs tuckerbox args on m: this machine's own binary, or over
// SSH or herdr-core as its [machines] entry says.
func (a App) dispatchOn(ctx context.Context, repo runs.Repo, cfg *config.Config, m dispatch.Machine, args []string, timeout time.Duration) ([]byte, error) {
	if !m.Local {
		e, ok := cfg.Machines[m.Name]
		if !ok {
			return nil, fmt.Errorf("%s is not in [machines]", m.Name)
		}
		if e.Name == "" {
			e.Name = m.Name
		}
		return a.machineRunner().Run(ctx, e, args, timeout)
	}
	exe, err := a.executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = repo.Root
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: no answer within %s: %w", m.Name, timeout, machines.ErrUnreachable)
		}
		return out, &machines.Error{Machine: m.Name, Stderr: stderr.String(), Err: err}
	}
	return out, nil
}

// machineRunner is how commands reach other machines; tests point it at
// fake ssh and hc programs.
func (a App) machineRunner() machines.Runner {
	return a.MachineRunner
}
