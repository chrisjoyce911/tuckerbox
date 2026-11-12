// Package board derives a repository's board from OpenSpec and run state.
// It owns no persistent state. Version 1 allows additive JSON fields only.
package board

import "github.com/chrisjoyce911/tuckerbox/internal/agent"

const Version = 1

type Document struct {
	Version         int      `json:"version"`
	Repo            string   `json:"repo"`
	Path            string   `json:"path"`
	Machine         string   `json:"machine"`
	RequireApproval bool     `json:"require_approval"`
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
}
type Detail struct {
	Change   string  `json:"change"`
	Proposal string  `json:"proposal"`
	Specs    []Spec  `json:"specs"`
	Tasks    []Task  `json:"tasks"`
	Rounds   []Round `json:"rounds"`
	History  []Event `json:"history"`
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
}
