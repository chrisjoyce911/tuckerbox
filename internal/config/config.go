// Package config loads, merges and validates Tuckerbox configuration.
//
// Two files are read:
//
//   - the machine config: config.toml in $XDG_CONFIG_HOME/tuckerbox, else
//     ~/.config/tuckerbox on macOS and Linux, or %AppData%\tuckerbox on
//     Windows (see [MachinePath]); it is optional; and
//   - the repository config, .tuckerbox/config.toml at the repository root
//     (see [RepoFile]), which a run requires.
//
// A value set in the repository config overrides the machine value for the
// same key. Some keys are reserved to the machine and are refused in a
// repository config: permission profiles, raw agent arguments, the editor
// and agent_backend.
// One key is reserved to the repository and refused in machine config:
// require_approval, so every machine applies the same run gate.
// A repository can only refer to a permission profile by name, and the
// machine decides which repositories (by remote) may use it.
//
// Rules beyond the file format:
//
//   - Remotes. A profile is matched against the repository's origin remote,
//     or its only remote when there is no origin (see [Remote]). Remotes and
//     patterns are compared as host/path (see [NormalizeRemote]); "*" allows
//     any repository, including one with no remote, and other patterns use
//     path.Match, so "github.com/owner/*" allows every repository of owner.
//   - Profiles grant flags per agent ([Profile.Args]). A step whose profile
//     grants no flags for the step's agent is invalid.
//   - Agents. A repository may define new agents, but may not redefine the
//     built-ins (claude, codex) and may not put permission flags such as
//     --dangerously-skip-permissions or --sandbox in a command; machine
//     config may override a built-in's template.
//   - Loops. on_reject names the same step or an earlier one; max_rounds
//     without on_reject is invalid; a [steps.<id>] table in the repository
//     config that is not listed in workflow.steps is unreachable and invalid.
//   - Defaults. max_parallel is [DefaultMaxParallel] (1), max_rounds is
//     [DefaultMaxRounds] (3), ship is "local", worktrees go to
//     [DefaultWorktreePath] on branch [DefaultBranch], and the base branch
//     is the remote's default branch.
//
// [Load] returns the merged result as a [Config], which is what later
// packages (runs, agents) consume, together with every [Problem] found.
package config

import (
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"path/filepath"
	"slices"
	"time"
)

// RepoDir is the folder at the repository root that holds Tuckerbox files:
// config.toml and role prompt overrides in roles/<role>.md.
const RepoDir = ".tuckerbox"

// RepoFile is the repository config path relative to the repository root.
var RepoFile = filepath.Join(RepoDir, "config.toml")

// DefaultMaxRounds is the round limit of an on_reject loop when a step does
// not set max_rounds.
const DefaultMaxRounds = 3

// DefaultMaxParallel is the number of changes that run at once per repository
// when neither config sets max_parallel.
const DefaultMaxParallel = 1

// DefaultWorktreePath and DefaultBranch are the worktree location and branch
// name patterns used when [worktree] does not set them. See [Worktree].
const (
	DefaultWorktreePath = "../{repo}-worktrees/{change}"
	DefaultBranch       = "{change}"
)

// BuiltinAgents are the agents Tuckerbox defines itself. The agents package
// owns their command templates. Machine config may override a built-in's
// template (it then appears in [Config.Agents]); repository config may not
// redefine a built-in at all.
var BuiltinAgents = []string{"claude", "codex"}

// BuiltinRoles are the roles with a built-in prompt. A repository may add or
// override roles with .tuckerbox/roles/<role>.md.
var BuiltinRoles = []string{"planner", "builder", "reviewer", "fixer"}

// Placeholders are the names an agent command template may use, written as
// {name} inside an argument. A command must pass {brief} or {brief_file}.
//
//   - {brief}: the brief's text, what the agent is told to do; when the
//     command would be too long with it inline, an instruction to read the
//     brief file instead
//   - {brief_file}: path of the brief file, for an agent that reads its
//     prompt from a file
//   - {workdir}: the change's worktree
//   - {model}: the step's model, or the agent's default model
//   - {effort}: the step's effort, or the agent's default effort
//   - {lastmessage}: a file, owned by Tuckerbox, where the agent CLI writes
//     the agent's final message; without it the final message is read from
//     the agent's standard output
var Placeholders = []string{"brief", "brief_file", "workdir", "model", "effort", "lastmessage"}

// ShipMode says what happens when a run finishes its last step.
type ShipMode string

// ShipLocal leaves the work committed on the change's branch and pushes
// nothing. ShipMR and ShipMerge opt into the forge shipping stage.
const (
	ShipLocal ShipMode = "local"
	ShipMR    ShipMode = "mr"
	ShipMerge ShipMode = "merge"
)

// Config is the resolved configuration for one repository on this machine:
// the machine config merged with the repository config, defaults filled in.
//
// A Config returned by [Load] together with problems may be incomplete; use
// it only for display when problems were reported.
type Config struct {
	// RepoRoot is the repository (or worktree) root the config was loaded for.
	RepoRoot string
	// RepoPath is the repository config file that was read.
	RepoPath string
	// MachinePath is the machine config file that was read, or "" if there
	// was none.
	MachinePath string
	// Remote is the repository remote that permission profiles were matched
	// against (origin, or the only remote; see [Remote]), normalised by
	// [NormalizeRemote]; "" if the repository has none.
	Remote string

	// Steps is the workflow, in run order. Every step's fields are resolved:
	// MaxRounds is set, Agent and Role are non-empty.
	Steps []Step
	// MaxParallel is the most changes from this repository that run at once.
	MaxParallel int
	// Ship is the ship mode.
	Ship        ShipMode
	Shipping    Shipping
	Forge       forge.Settings
	ForgeTarget forge.Target
	Forges      map[string]ForgeHost
	Machine     string
	FixSteps    map[string]Step
	// Worktree holds the worktree and branch settings for runs.
	Worktree Worktree
	// BoardColumns defaults to compact; steps shows each workflow step separately.
	BoardColumns BoardColumns
	// Autopilot holds the [autopilot] settings, defaults filled in.
	Autopilot Autopilot

	// Agents holds the agent command templates defined in config, by name:
	// custom agents from either file and machine overrides of built-ins. An
	// agent named by a step is either here or in [BuiltinAgents]. A
	// repository agent overrides a machine agent of the same name, never a
	// built-in, and its command holds no permission flags.
	Agents map[string]Agent
	// Permissions holds the machine's permission profiles, by name. Only
	// profiles from machine config ever appear here.
	Permissions map[string]Profile
	// Editor is the command used to open files for the user. Machine only;
	// "" if unset.
	Editor string
	// AgentBackend is how step agents run: [BackendAuto], [BackendTerminal]
	// or [BackendHeadless]. Machine only; BackendAuto unless set.
	AgentBackend string
	// RequireApproval makes tuckerbox run refuse a change that is not
	// approved (see package approval). Repository only; true unless the
	// repository config sets require_approval = false.
	RequireApproval bool
}

// Autopilot is the [autopilot] table: how tuckerbox autopilot works
// through changes. Either file may set it; repository values win.
type Autopilot struct {
	// Scope is AutopilotGroup (each change group by group, the default)
	// or AutopilotChange (whole changes).
	Scope string
	// Interval is the time between full passes (default 60s, at least 10s).
	Interval time.Duration
}

// Autopilot scopes and limits.
const (
	AutopilotGroup           = "group"
	AutopilotChange          = "change"
	DefaultAutopilotInterval = 60 * time.Second
	MinAutopilotInterval     = 10 * time.Second
)

// BoardColumns selects the board layout. Repository values override machine defaults.
type BoardColumns string

const (
	BoardCompact BoardColumns = "compact"
	BoardSteps   BoardColumns = "steps"
)

// Step is one step of the workflow.
type Step struct {
	// ID is the step's name, as listed in workflow.steps.
	ID string
	// Agent names the agent that runs the step: a built-in or a key of
	// [Config.Agents].
	Agent string
	// Role names the role prompt: a built-in role or .tuckerbox/roles/<role>.md.
	Role string
	// Permissions names a machine permission profile, or "" for the agent's
	// own defaults. When set, the profile exists in [Config.Permissions],
	// allows this repository's remote and grants flags for Agent.
	Permissions string
	// Model and Effort fill the {model} and {effort} placeholders. When ""
	// the agent's defaults apply.
	Model, Effort string
	// OnReject names the step to go back to when this step reports
	// rejected, or "" if this step does not loop. It is this step or an
	// earlier one in the workflow; a later step is invalid.
	OnReject string
	// MaxRounds is the limit of rounds of the on_reject loop; after that many
	// rejections the run is blocked. It is DefaultMaxRounds unless set, and
	// setting it without OnReject is invalid.
	MaxRounds int
}

// Agent is a command template for an agent CLI defined in config.
type Agent struct {
	// Name is the agent's name.
	Name string
	// Command is the program and its arguments. Arguments may hold
	// placeholders (see [Placeholders]); Command[0] is the program.
	Command []string
	// Model and Effort are defaults for the {model} and {effort}
	// placeholders when a step does not set them.
	Model, Effort string
	// FromRepo is true when the repository config defined the agent.
	FromRepo bool
}

// Profile is a machine permission profile: the flags it grants each agent,
// and the repositories that may use it.
type Profile struct {
	// Name is the profile's name, as steps refer to it.
	Name string
	// Args maps an agent name to the extra arguments the profile adds to
	// that agent's command. A step may use the profile only with an agent
	// that has an entry here.
	Args map[string][]string
	// AllowedRemotes lists the repository remotes that may use the profile,
	// normalised by [NormalizeRemote]. An entry may be "*" (any repository,
	// including one with no remote) or a path.Match pattern such as
	// "github.com/owner/*". A repository with no remote matches only "*".
	AllowedRemotes []string
}

// Worktree holds where runs put their worktrees and what they name branches.
// Patterns may use {change} (the change id) and {repo} (the repository
// folder's name).
type Worktree struct {
	// Base is the branch new change branches start from. "" (the default)
	// means the remote default branch after a fetch, or the local default
	// branch if there is no remote.
	Base string
	// Path is the worktree location pattern, [DefaultWorktreePath] unless
	// set. A relative path is relative to the repository root. It contains
	// {change}.
	Path string
	// Branch is the branch name pattern, [DefaultBranch] unless set. It
	// contains {change}.
	Branch string
}

// Step returns the step with the given id.
func (c *Config) Step(id string) (Step, bool) {
	for _, s := range c.Steps {
		if s.ID == id {
			return s, true
		}
	}
	s, ok := c.FixSteps[id]
	return s, ok
}

// PermissionArgs returns the extra arguments the step's permission profile
// grants its agent, or nil when the step names no profile.
func (c *Config) PermissionArgs(s Step) []string {
	if s.Permissions == "" {
		return nil
	}
	return slices.Clone(c.Permissions[s.Permissions].Args[s.Agent])
}

// IsBuiltinAgent reports whether name is a built-in agent.
func IsBuiltinAgent(name string) bool { return slices.Contains(BuiltinAgents, name) }
