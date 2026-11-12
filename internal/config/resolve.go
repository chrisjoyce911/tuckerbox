package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// nameRE is what step, role, agent and profile names may look like. Role
// names become file names, so they must not hold path separators.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

var placeholderRE = regexp.MustCompile(`\{([^{}]*)\}`)

// bypassFlags are agent flags that change permission prompts, sandboxing or
// approvals. A repository-defined agent command may not contain them.
var bypassFlags = []string{
	"--dangerously-skip-permissions", "--permission-mode", "--allowedTools", "--allowed-tools",
	"--dangerously-bypass-approvals-and-sandbox", "--yolo", "--full-auto", "--sandbox", "--ask-for-approval",
}

// pick returns the repository value if set, else the machine value, and the
// file it came from.
func pick[T any](r, m *T, rf, mf string) (v T, from string, ok bool) {
	if r != nil {
		return *r, rf, true
	}
	if m != nil {
		return *m, mf, true
	}
	return v, "", false
}

func resolve(root, remote string, m, r *file) (*Config, Problems) {
	if m == nil {
		m = &file{defined: func(...string) bool { return false }}
	}
	var ps Problems
	ps = append(ps, m.problems...)
	ps = append(ps, r.problems...)
	add := func(file, key, format string, args ...any) {
		ps = append(ps, Problem{File: file, Key: key, Message: fmt.Sprintf(format, args...)})
	}

	cfg := &Config{
		RepoRoot:    root,
		Remote:      NormalizeRemote(remote),
		Agents:      map[string]Agent{},
		Permissions: map[string]Profile{},
	}
	mr, rr := &m.raw, &r.raw

	// Scalars.
	if v, from, ok := pick(rr.MaxParallel, mr.MaxParallel, r.path, m.path); ok {
		if v < 1 {
			add(from, "max_parallel", "must be at least 1, got %d", v)
		}
		cfg.MaxParallel = v
	} else {
		cfg.MaxParallel = DefaultMaxParallel
	}
	if v, from, ok := pick(rr.Ship, mr.Ship, r.path, m.path); ok {
		switch ShipMode(v) {
		case ShipLocal, ShipMR, ShipMerge:
		default:
			add(from, "ship", "unknown ship mode %q; use local, mr or merge", v)
		}
		cfg.Ship = ShipMode(v)
	} else {
		cfg.Ship = ShipLocal
	}
	if mr.Editor != nil {
		cfg.Editor = *mr.Editor
	}
	cfg.AgentBackend = BackendAuto
	if mr.AgentBackend != nil {
		cfg.AgentBackend = *mr.AgentBackend
		if err := CheckBackend(cfg.AgentBackend); err != nil {
			add(m.path, "agent_backend", "%v", err)
		}
	}
	cfg.RequireApproval = rr.RequireApproval == nil || *rr.RequireApproval

	cfg.BoardColumns = BoardCompact
	if v, from, ok := pick(rr.Board.Columns, mr.Board.Columns, r.path, m.path); ok {
		cfg.BoardColumns = BoardColumns(v)
		if cfg.BoardColumns != BoardCompact && cfg.BoardColumns != BoardSteps {
			add(from, "board.columns", "unknown board layout %q; use \"compact\" or \"steps\"", v)
		}
	}

	cfg.Autopilot = Autopilot{Scope: AutopilotGroup, Interval: DefaultAutopilotInterval}
	if v, from, ok := pick(rr.Autopilot.Scope, mr.Autopilot.Scope, r.path, m.path); ok {
		cfg.Autopilot.Scope = v
		if v != AutopilotGroup && v != AutopilotChange {
			add(from, "autopilot.scope", "unknown scope %q; use \"group\" or \"change\"", v)
		}
	}
	if v, from, ok := pick(rr.Autopilot.Interval, mr.Autopilot.Interval, r.path, m.path); ok {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			add(from, "autopilot.interval", "%q is not a duration such as \"60s\" or \"2m\"", v)
		case d < MinAutopilotInterval:
			add(from, "autopilot.interval", "%s is too short; use at least %s", v, MinAutopilotInterval)
		default:
			cfg.Autopilot.Interval = d
		}
	}

	// Worktree settings.
	cfg.Worktree.Base, _, _ = pick(rr.Worktree.Base, mr.Worktree.Base, r.path, m.path)
	for _, w := range []struct {
		key      string
		r, m     *string
		dst      *string
		fallback string
	}{
		{"worktree.path", rr.Worktree.Path, mr.Worktree.Path, &cfg.Worktree.Path, DefaultWorktreePath},
		{"worktree.branch", rr.Worktree.Branch, mr.Worktree.Branch, &cfg.Worktree.Branch, DefaultBranch},
	} {
		v, from, ok := pick(w.r, w.m, r.path, m.path)
		if !ok {
			*w.dst = w.fallback
			continue
		}
		*w.dst = v
		names := placeholders(v)
		if !slices.Contains(names, "change") {
			add(from, w.key, "must contain {change}, so each change gets its own")
		}
		for _, n := range names {
			if n != "change" && n != "repo" {
				add(from, w.key, "unknown placeholder {%s}; use {change} or {repo}", n)
			}
		}
	}

	resolveShipping(cfg, m, r, add)

	// Agents: machine first, then repository additions.
	for _, name := range slices.Sorted(maps.Keys(mr.Agents)) {
		cfg.Agents[name] = checkAgent(m.path, name, mr.Agents[name], false, add)
	}
	for _, name := range slices.Sorted(maps.Keys(rr.Agents)) {
		if IsBuiltinAgent(name) {
			add(r.path, "agents."+name, "%q is a built-in agent; repository config may not redefine it (machine config may)", name)
			continue
		}
		cfg.Agents[name] = checkAgent(r.path, name, rr.Agents[name], true, add)
	}
	knownAgent := func(name string) bool {
		_, ok := cfg.Agents[name]
		return ok || IsBuiltinAgent(name)
	}

	// Permission profiles: machine only (parse dropped any from the repo).
	for _, name := range slices.Sorted(maps.Keys(mr.Permissions)) {
		raw := mr.Permissions[name]
		key := "permissions." + name
		if !nameRE.MatchString(name) {
			add(m.path, key, "invalid profile name %q", name)
		}
		p := Profile{Name: name, Args: map[string][]string{}}
		if len(raw.AllowedRemotes) == 0 {
			add(m.path, key+".allowed_remotes", "missing; list the repository remotes that may use this profile, or \"*\" for any")
		}
		for _, rem := range raw.AllowedRemotes {
			if rem == "*" {
				p.AllowedRemotes = append(p.AllowedRemotes, rem)
			} else {
				p.AllowedRemotes = append(p.AllowedRemotes, NormalizeRemote(rem))
			}
		}
		if len(raw.Args) == 0 {
			add(m.path, key+".args", "missing; give the flags this profile adds per agent, such as args.claude = [\"--permission-mode\", \"acceptEdits\"]")
		}
		for _, agent := range slices.Sorted(maps.Keys(raw.Args)) {
			if !knownAgent(agent) {
				add(m.path, key+".args."+agent, "unknown agent %q", agent)
			}
			p.Args[agent] = slices.Clone(raw.Args[agent])
		}
		cfg.Permissions[name] = p
	}

	// Workflow.
	order, orderFrom, ok := pick(nilIfUndefined(r, rr.Workflow.Steps), nilIfUndefined(m, mr.Workflow.Steps), r.path, m.path)
	if !ok || len(order) == 0 {
		file := r.path
		if ok {
			file = orderFrom
		}
		add(file, "workflow.steps", "no steps; list the workflow's steps in run order, such as steps = [\"plan\", \"build\", \"review\"]")
	}
	index := map[string]int{}
	for i, id := range order {
		if !nameRE.MatchString(id) {
			add(orderFrom, "workflow.steps", "invalid step id %q", id)
		}
		if _, dup := index[id]; dup {
			add(orderFrom, "workflow.steps", "step %q is listed twice", id)
			continue
		}
		index[id] = i
	}
	resolveFixSteps(cfg, root, m, r, index, knownAgent, add)
	cfg.Overrides = resolveOverrides(r, add)

	// Step tables the workflow never reaches.
	for _, f := range []*file{r, m} {
		if f == m && orderFrom != m.path {
			continue // machine steps are defaults; a repo workflow may use only some
		}
		for _, id := range slices.Sorted(maps.Keys(f.raw.Steps)) {
			if _, ok := index[id]; !ok && id != cfg.Shipping.FixCI && id != cfg.Shipping.FixReview {
				add(f.path, "steps."+id, "step is not in workflow.steps, so no run reaches it")
			}
		}
	}

	for i, id := range order {
		if index[id] != i {
			continue // duplicate, reported above
		}
		cfg.Steps = append(cfg.Steps, resolveStep(cfg, root, id, i, index, mr.Steps[id], rr.Steps[id], m.path, r.path, orderFrom, knownAgent, add))
	}
	// A workflow id without its required definition is not a usable loop
	// target. Name the referring field as well as the missing step fields.
	for _, step := range cfg.Steps {
		if step.OnReject == "" {
			continue
		}
		target, ok := cfg.Step(step.OnReject)
		if ok && (target.Agent == "" || target.Role == "") {
			_, from, _ := pick(rr.Steps[step.ID].OnReject, mr.Steps[step.ID].OnReject, r.path, m.path)
			add(from, "steps."+step.ID+".on_reject", "step %q has no complete definition", step.OnReject)
		}
	}
	return cfg, ps
}

// nilIfUndefined returns nil when the file does not set workflow.steps, so
// that an explicitly empty list still counts as set.
func nilIfUndefined(f *file, v []string) *[]string {
	if !f.defined("workflow", "steps") {
		return nil
	}
	return &v
}

// source says which file set a string step key: the repository's if r
// is set to a non-empty value, else the machine's, else neither ("").
func source(r, m *string) Source {
	switch {
	case r != nil && *r != "":
		return SourceRepo
	case r != nil:
		return ""
	case m != nil && *m != "":
		return SourceMachine
	}
	return ""
}

type addFunc func(file, key, format string, args ...any)

func resolveStep(cfg *Config, root, id string, i int, index map[string]int, ms, rs rawStep, mf, rf, orderFrom string, knownAgent func(string) bool, add addFunc) Step {
	key := "steps." + id
	s := Step{ID: id}

	agent, agentFrom, ok := pick(rs.Agent, ms.Agent, rf, mf)
	switch {
	case !ok || agent == "":
		add(firstNonEmpty(agentFrom, orderFrom), key+".agent", "missing; name the agent that runs this step (%s, or one defined under [agents])", strings.Join(BuiltinAgents, ", "))
	case !knownAgent(agent):
		add(agentFrom, key+".agent", "unknown agent %q; use %s, or define it under [agents.%s]", agent, strings.Join(BuiltinAgents, ", "), agent)
	}
	s.Agent = agent

	role, roleFrom, ok := pick(rs.Role, ms.Role, rf, mf)
	switch {
	case !ok || role == "":
		add(firstNonEmpty(roleFrom, orderFrom), key+".role", "missing; use %s, or add %s", strings.Join(BuiltinRoles, ", "), filepath.ToSlash(filepath.Join(RepoDir, "roles", "<role>.md")))
	case !nameRE.MatchString(role):
		add(roleFrom, key+".role", "invalid role name %q", role)
	case !slices.Contains(BuiltinRoles, role) && !roleFileExists(root, role):
		add(roleFrom, key+".role", "unknown role %q; use %s, or add %s", role, strings.Join(BuiltinRoles, ", "), filepath.ToSlash(filepath.Join(RepoDir, "roles", role+".md")))
	}
	s.Role = role

	s.Model, _, _ = pick(rs.Model, ms.Model, rf, mf)
	s.Effort, _, _ = pick(rs.Effort, ms.Effort, rf, mf)
	s.From = StepSources{
		Agent:       source(rs.Agent, ms.Agent),
		Model:       source(rs.Model, ms.Model),
		Effort:      source(rs.Effort, ms.Effort),
		Permissions: source(rs.Permissions, ms.Permissions),
	}

	if prof, from, ok := pick(rs.Permissions, ms.Permissions, rf, mf); ok && prof != "" {
		s.Permissions = prof
		if msg := cfg.ProfileProblem(prof, agent); msg != "" {
			add(from, key+".permissions", "%s", msg)
		}
	}

	onReject, rejectFrom, hasReject := pick(rs.OnReject, ms.OnReject, rf, mf)
	if hasReject && onReject != "" {
		j, known := index[onReject]
		switch {
		case !known:
			add(rejectFrom, key+".on_reject", "unknown step %q", onReject)
		case j > i:
			add(rejectFrom, key+".on_reject", "step %q comes after %q; on_reject must go back to this step or an earlier one", onReject, id)
		}
		s.OnReject = onReject
	}
	rounds, roundsFrom, ok := pick(rs.MaxRounds, ms.MaxRounds, rf, mf)
	switch {
	case !ok:
		s.MaxRounds = DefaultMaxRounds
	case rounds < 1:
		add(roundsFrom, key+".max_rounds", "must be at least 1, got %d", rounds)
		s.MaxRounds = rounds
	case s.OnReject == "":
		add(roundsFrom, key+".max_rounds", "set without on_reject; max_rounds limits an on_reject loop")
		s.MaxRounds = rounds
	default:
		s.MaxRounds = rounds
	}
	return s
}

func checkAgent(path, name string, raw rawAgent, fromRepo bool, add addFunc) Agent {
	key := "agents." + name
	if !nameRE.MatchString(name) {
		add(path, key, "invalid agent name %q", name)
	}
	a := Agent{Name: name, Command: slices.Clone(raw.Command), Interactive: slices.Clone(raw.Interactive),
		Model: raw.Model, Effort: raw.Effort, FromRepo: fromRepo}
	if raw.Interactive != nil {
		checkInteractive(path, key+".interactive", raw.Interactive, fromRepo, add)
	}
	if len(raw.Command) == 0 || raw.Command[0] == "" {
		add(path, key+".command", "missing; give the program and its arguments, such as command = [%q, \"{brief}\"]", name)
		return a
	}
	var seen []string
	for _, arg := range raw.Command {
		for _, n := range placeholders(arg) {
			if !slices.Contains(Placeholders, n) {
				add(path, key+".command", "unknown placeholder {%s}; use {%s}", n, strings.Join(Placeholders, "}, {"))
			}
			seen = append(seen, n)
		}
		if fromRepo {
			flag, _, _ := strings.Cut(arg, "=")
			if slices.Contains(bypassFlags, flag) {
				add(path, key+".command", "%s is a permission flag; repository config may not grant permissions, use a permission profile in machine config", flag)
			}
		}
	}
	if !slices.Contains(seen, "brief") && !slices.Contains(seen, "brief_file") {
		add(path, key+".command", "must pass {brief} (the brief's text) or {brief_file} (the brief file's path)")
	}
	return a
}

// checkInteractive checks an agent's interactive command: a program, known
// placeholders, {turnhook} and {brief} or {brief_file}, and no permission
// flags from repository config.
func checkInteractive(path, key string, argv []string, fromRepo bool, add addFunc) {
	if len(argv) == 0 || argv[0] == "" {
		add(path, key, "empty; give the program and its arguments, or leave interactive out to run the agent headless")
		return
	}
	var seen []string
	for _, arg := range argv {
		for _, n := range placeholders(arg) {
			if !slices.Contains(InteractivePlaceholders, n) {
				add(path, key, "unknown placeholder {%s}; use {%s}", n, strings.Join(InteractivePlaceholders, "}, {"))
			}
			seen = append(seen, n)
		}
		if fromRepo {
			flag, _, _ := strings.Cut(arg, "=")
			if slices.Contains(bypassFlags, flag) {
				add(path, key, "%s is a permission flag; repository config may not grant permissions, use a permission profile in machine config", flag)
			}
		}
	}
	if !slices.Contains(seen, "turnhook") {
		add(path, key, "must pass {turnhook}, the command the agent runs at the end of each turn with its final message")
	}
	if !slices.Contains(seen, "brief") && !slices.Contains(seen, "brief_file") {
		add(path, key, "must pass {brief} (the first prompt) or {brief_file} (the brief file's path)")
	}
}

func placeholders(s string) []string {
	var out []string
	for _, m := range placeholderRE.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func roleFileExists(root, role string) bool {
	_, err := os.Stat(filepath.Join(root, RepoDir, "roles", role+".md"))
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// resolveOverrides reads the repository's [overrides] allow-list. Names
// must be non-empty and listed once; profile names must be valid names. A
// listed profile need not exist on this machine: a change that picks it is
// refused where it is missing, as a repository step would be.
func resolveOverrides(r *file, add addFunc) *Overrides {
	raw := r.raw.Overrides
	if raw == nil {
		return nil
	}
	list := func(key string, vs []string, isName bool) []string {
		seen := map[string]bool{}
		var out []string
		for _, v := range vs {
			switch {
			case strings.TrimSpace(v) == "":
				add(r.path, key, "empty value; list the names a change may choose")
			case isName && !nameRE.MatchString(v):
				add(r.path, key, "invalid name %q", v)
			case seen[v]:
				add(r.path, key, "%q is listed twice", v)
			default:
				seen[v] = true
				out = append(out, v)
			}
		}
		return out
	}
	o := &Overrides{
		Agents:      list("overrides.agents", raw.Agents, true),
		Efforts:     list("overrides.efforts", raw.Efforts, false),
		Permissions: list("overrides.permissions", raw.Permissions, true),
	}
	if len(raw.Models) > 0 {
		o.Models = map[string][]string{}
		for _, agent := range slices.Sorted(maps.Keys(raw.Models)) {
			if !nameRE.MatchString(agent) {
				add(r.path, "overrides.models", "invalid agent name %q", agent)
				continue
			}
			o.Models[agent] = list("overrides.models."+agent, raw.Models[agent], false)
		}
	}
	return o
}

// RepoOverrides reads the [overrides] allow-list from data, the contents
// of a repository config file at path (as one checkout or commit has it),
// for judging a change's overrides against that copy rather than the
// loaded config's. It returns nil when the file has no [overrides] table.
// Problems are reported only when the file cannot be decoded or the
// table itself is invalid; the rest of the file is not judged here.
func RepoOverrides(path string, data []byte) (*Overrides, Problems) {
	f := parse(path, string(data), true)
	if f.broken {
		return nil, f.problems
	}
	var ps Problems
	for _, p := range f.problems {
		if p.Key == "overrides" || strings.HasPrefix(p.Key, "overrides.") {
			ps = append(ps, p)
		}
	}
	o := resolveOverrides(f, func(file, key, format string, args ...any) {
		ps = append(ps, Problem{File: file, Key: key, Message: fmt.Sprintf(format, args...)})
	})
	if len(ps) > 0 {
		return nil, ps
	}
	return o, nil
}
