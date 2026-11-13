// Package board derives a repository's board from OpenSpec and run state.
// It owns no persistent state. Version 1 allows additive JSON fields only.
package board

import (
	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
)

const Version = 1

type Document struct {
	Ship    string `json:"ship"`
	Version int    `json:"version"`
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Machine string `json:"machine"`
	// OS is the operating system Tuckerbox runs on there (Go's GOOS):
	// the board picks how to run a command on that machine by it.
	OS              string `json:"os"`
	RequireApproval bool   `json:"require_approval"`
	// Autopilot says whether tuckerbox autopilot runs for the repository
	// on this machine (add-autopilot).
	Autopilot Autopilot `json:"autopilot"`
	// Mode is the repository's operating mode: solo, fleet or team
	// (modes-board: Mode in the board document).
	Mode string `json:"mode"`
	// Fleet is fleet mode's limit and the claims counted against it.
	Fleet *FleetCount `json:"fleet,omitempty"`
	// ClaimsRead says where fleet or team claims came from, and when.
	ClaimsRead *ClaimsRead `json:"claims_read,omitempty"`
	Steps     []string  `json:"steps"`
	Columns   []Column  `json:"columns"`
	Cards     []Card    `json:"cards"`
	Detail    *Detail   `json:"detail,omitempty"`
}
type Column struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Step string `json:"step,omitempty"`
	Name string `json:"name"`
}
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
type Approval struct {
	State     string `json:"state"`
	By        string `json:"by"`
	At        string `json:"at"`
	Committed bool   `json:"committed"`
	// Published is false while this machine has an approve or unapprove of
	// the change not yet on the remote (approval-publish).
	Published bool `json:"published"`
}
type Dependency struct {
	Change string `json:"change"`
	Column string `json:"column"`
}
type Card struct {
	MR         *ship.MRInfo `json:"mr,omitempty"`
	CI         *ship.CIInfo `json:"ci,omitempty"`
	ShipRound  int          `json:"ship_round,omitempty"`
	MaxRounds  int          `json:"max_rounds,omitempty"`
	Error      string       `json:"error,omitempty"`
	Change     string       `json:"change"`
	Title      string       `json:"title"`
	Column     string       `json:"column"`
	Tasks      Progress     `json:"tasks"`
	Approval   Approval     `json:"approval"`
	State      string       `json:"state"`
	Reason     string       `json:"reason"`
	Badge      string       `json:"badge"`
	Step       string       `json:"step"`
	Agent      string       `json:"agent"`
	Round      int          `json:"round"`
	Since      string       `json:"since"`
	Branch     string       `json:"branch"`
	Worktree   string       `json:"worktree"`
	Machine    string       `json:"machine"`
	EditFolder string       `json:"edit_folder"`
	DependsOn  []Dependency `json:"depends_on"`
	Archived   string       `json:"archived,omitempty"`
	// Priority is the change's stored priority in the repository checkout
	// on this machine, or null; Position is its place in priority order,
	// from 1. Both only for active changes (board-data: Priority on
	// the board).
	Priority *int64 `json:"priority"`
	Position int    `json:"position,omitempty"`
	// Scope is the card's run's task scope; absent for the whole change.
	Scope *scope.Scope `json:"scope,omitempty"`
	// Activity is what the run's agent is doing, on cards whose run is
	// running, queued or stopped (agent-activity).
	Activity *Activity `json:"activity,omitempty"`
	// Question is what the run asks the person: on a blocked card, its
	// step's blocked result (summary and findings, step and round) and the
	// answers given so far; on a card whose terminal agent waits for the
	// person, kind "waiting" and the agent's last message
	// (blocked-answers: The question of a blocked run).
	Question *runs.QuestionView `json:"question,omitempty"`
	// Settings is what each workflow step runs with, in workflow order,
	// with the source of each value (settings-board: Effective settings on
	// cards). A running step shows what it started with. Empty for
	// archived cards.
	Settings []StepSettings `json:"settings"`
	// Machines is the change's machines list from its tuckerbox.toml.
	Machines []string `json:"machines"`
	// Claim is the change's claim in fleet or team mode (modes-board:
	// Claims on cards); absent when it has none.
	Claim *Claim `json:"claim,omitempty"`
	// NeedsYou says why the card needs a person; absent when it doesn't.
	NeedsYou string `json:"needs_you,omitempty"`
}

// StepSettings is one workflow step's effective settings on a card.
type StepSettings struct {
	Step        string  `json:"step"`
	Agent       Setting `json:"agent"`
	Model       Setting `json:"model"`
	Effort      Setting `json:"effort"`
	Permissions Setting `json:"permissions"`
	// Started is true when these are the values the run's current step
	// started with, recorded in run.json, rather than what the step
	// would run with now.
	Started bool `json:"started,omitempty"`
}

// Setting is one effective value and where it came from: "change",
// "repo", "machine" or "default". Value is "" when the agent's own
// default applies (or, for permissions, no profile).
type Setting struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// Choices is what card detail may offer a change's settings pickers: the
// repository's [overrides] allow-list, and the step keys whose change
// voids an approval. All empty when the repository has no [overrides].
type Choices struct {
	Agents []string `json:"agents"`
	// Models lists the allowed models per agent name.
	Models        map[string][]string `json:"models"`
	Efforts       []string            `json:"efforts"`
	Permissions   []string            `json:"permissions"`
	VoidsApproval []string            `json:"voids_approval"`
}

// Autopilot is tuckerbox autopilot for the board's repository on this
// machine: running only while its lock is held.
type Autopilot struct {
	Running bool   `json:"running"`
	Pid     int    `json:"pid,omitempty"`
	Machine string `json:"machine,omitempty"`
	Started string `json:"started,omitempty"`
	// Terminal is the herdr-core terminal it runs in, when it runs in
	// one (add-herdr-topbar: Autopilot in the board document).
	Terminal string `json:"terminal,omitempty"`
}
type Detail struct {
	Change   string  `json:"change"`
	Proposal string  `json:"proposal"`
	Specs    []Spec  `json:"specs"`
	Tasks    []Task  `json:"tasks"`
	Rounds   []Round `json:"rounds"`
	History  []Event `json:"history"`
	// Groups are tasks.md's task groups, in order.
	Groups []Group `json:"groups"`
	// Ticks are the current run's task_ticked and task_unticked events,
	// newest first.
	Ticks []Tick `json:"ticks"`
	// Choices is the allow-list for the change's settings pickers.
	Choices Choices `json:"choices"`
}

// Group is a task group in card detail. Ticked and Total count its tasks
// other than (ship) tasks, which shipping ticks: a group with Ticked <
// Total has tasks a run can still take on.
type Group struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Ticked int    `json:"ticked"`
	Total  int    `json:"total"`
}
type Spec struct {
	Capability string `json:"capability"`
	Text       string `json:"text"`
}
type Task struct {
	Number  string `json:"number"`
	Group   string `json:"group"`
	Text    string `json:"text"`
	Checked bool   `json:"checked"`
}
type Round struct {
	Round    int             `json:"round"`
	Result   string          `json:"result"`
	Step     string          `json:"step"`
	Summary  string          `json:"summary"`
	Findings []agent.Finding `json:"findings"`
	Ended    string          `json:"ended"`
	// Scope is the task scope the round ran with; "" for the whole change.
	Scope string `json:"scope"`
}
type Event struct {
	Commit    string          `json:"commit,omitempty"`
	Branch    string          `json:"branch,omitempty"`
	Link      string          `json:"link,omitempty"`
	Number    int             `json:"number,omitempty"`
	Method    string          `json:"method,omitempty"`
	ShipRound int             `json:"ship_round,omitempty"`
	CI        string          `json:"ci,omitempty"`
	Jobs      []string        `json:"jobs,omitempty"`
	Threads   []string        `json:"threads,omitempty"`
	At        string          `json:"at"`
	Event     string          `json:"event"`
	Step      string          `json:"step"`
	Round     int             `json:"round"`
	Agent     string          `json:"agent"`
	Machine   string          `json:"machine"`
	Result    string          `json:"result,omitempty"`
	To        string          `json:"to,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Findings  []agent.Finding `json:"findings,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Scope     string          `json:"scope,omitempty"`
	OldScope  string          `json:"old_scope,omitempty"`
	// Task is the task number of task_ticked and task_unticked.
	Task string `json:"task,omitempty"`
}
