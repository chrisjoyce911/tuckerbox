// Package board derives a repository's board from OpenSpec and run state.
// It owns no persistent state. Version 1 allows additive JSON fields only.
package board

import (
	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

const Version = 1

type Document struct {
	Version         int      `json:"version"`
	Repo            string   `json:"repo"`
	Path            string   `json:"path"`
	Machine         string   `json:"machine"`
	RequireApproval bool     `json:"require_approval"`
	Steps           []string `json:"steps"`
	Columns         []Column `json:"columns"`
	Cards           []Card   `json:"cards"`
	Detail          *Detail  `json:"detail,omitempty"`
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
}
type Dependency struct {
	Change string `json:"change"`
	Column string `json:"column"`
}
type Card struct {
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
	// from 1. Both only for active changes (change-priority: Priority on
	// the board).
	Priority *int64 `json:"priority"`
	Position int    `json:"position,omitempty"`
	// Scope is the card's run's task scope; absent for the whole change.
	Scope *scope.Scope `json:"scope,omitempty"`
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
	At       string          `json:"at"`
	Event    string          `json:"event"`
	Step     string          `json:"step"`
	Round    int             `json:"round"`
	Agent    string          `json:"agent"`
	Machine  string          `json:"machine"`
	Result   string          `json:"result,omitempty"`
	To       string          `json:"to,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Findings []agent.Finding `json:"findings,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Scope    string          `json:"scope,omitempty"`
	OldScope string          `json:"old_scope,omitempty"`
}
