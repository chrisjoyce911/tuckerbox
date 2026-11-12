package board

import (
	"path/filepath"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// needsYou is why a card needs a person (add-autopilot, autopilot-board:
// Cards that need a person), or "": its run is blocked or failed; it is
// ready with tasks left and its approval is stale, so its next group
// can't start; or autopilot runs whole changes and the ready run without
// a scope left tasks unticked.
func needsYou(r *runs.Run, approvalState, changeDir, autopilotScope string) string {
	switch r.State {
	case runs.Blocked, runs.Failed:
		if r.Reason != "" {
			return string(r.State) + ": " + r.Reason
		}
		return string(r.State)
	case runs.Ready:
	default:
		return ""
	}
	if r.Complete {
		return ""
	}
	tasks, err := runs.ReadTasks(filepath.Join(changeDir, "tasks.md"))
	if err != nil {
		return ""
	}
	left := scope.Unticked(tasks)
	if len(left) == 0 {
		return ""
	}
	if approvalState == "stale" {
		return "approve again to run the next group"
	}
	if autopilotScope == config.AutopilotChange && r.Scope == nil {
		return "tasks not ticked: " + strings.Join(left, ", ") + "; run it again by hand"
	}
	return ""
}
