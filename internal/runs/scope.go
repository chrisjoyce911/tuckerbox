package runs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// TasksFile is change's tasks.md in the checkout or worktree at root.
func TasksFile(root, change string) string {
	return filepath.Join(root, "openspec", "changes", change, "tasks.md")
}

// readTasks parses the tasks.md at p; a missing file has no tasks.
func readTasks(p string) ([]scope.Task, error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return scope.Parse(string(data)), nil
}

// ArchivedChange returns change's folder under openspec/changes/archive
// in root (named <date>-<change>, the latest if there are several), or ""
// when there is none.
func ArchivedChange(root, change string) string {
	dir := filepath.Join(root, "openspec", "changes", "archive")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	name := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-` + regexp.QuoteMeta(change) + `$`)
	var found []string
	for _, e := range entries {
		if e.IsDir() && name.MatchString(e.Name()) {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Strings(found)
	return filepath.Join(dir, found[len(found)-1])
}

// readChangeTasks reads change's tasks in the worktree at root. When the
// change's folder is gone and the change is in the archive there (a step
// archived it on its branch), it reads the archived tasks.md and reports
// archived.
func readChangeTasks(root, change string) (tasks []scope.Task, archived bool, err error) {
	active := filepath.Dir(TasksFile(root, change))
	if _, err := os.Stat(active); errors.Is(err, fs.ErrNotExist) {
		if folder := ArchivedChange(root, change); folder != "" {
			tasks, err := readTasks(filepath.Join(folder, "tasks.md"))
			return tasks, true, err
		}
	}
	tasks, err = readTasks(TasksFile(root, change))
	return tasks, false, err
}

// ScopeFor returns the scope a Start with spec gives the change, judged
// against tasksText (the tasks.md the run will use) and cur, the change's
// current run or nil. spec "" is the whole change (nil). When spec names
// the scope cur already has, cur's scope is returned as it is, unresolved:
// starting with the same scope continues the run as before. same reports
// that cur exists and has the same scope.
func ScopeFor(spec, tasksText string, cur *Run) (s *scope.Scope, same bool, err error) {
	var curSpec string
	if cur != nil {
		curSpec = cur.Scope.SpecOrEmpty()
	}
	if spec == "" {
		return nil, cur != nil && curSpec == "", nil
	}
	if spec != scope.Next && cur != nil && spec == curSpec {
		return cur.Scope, true, nil
	}
	s, err = scope.Resolve(spec, scope.Parse(tasksText))
	if err != nil {
		return nil, false, err
	}
	if cur != nil && s.Spec == curSpec {
		return cur.Scope, true, nil
	}
	return s, false, nil
}

// shipOutcome is what a run does once its last step is done.
type shipOutcome int

const (
	// outcomeReady: the run becomes ready; nothing is shipped.
	outcomeReady shipOutcome = iota
	// outcomeShip: the run goes on to ship as its mode says.
	outcomeShip
	// outcomeBlocked: the run is blocked, naming the unticked tasks.
	outcomeBlocked
)

// shipsToForge reports whether mode ships to a forge ("mr" or "merge"),
// where shipping ticks CI tasks once CI is green. In local mode (or no
// mode) CI tasks count like any other task.
func shipsToForge(mode config.ShipMode) bool {
	return mode != "" && mode != config.ShipLocal
}

// shipDecision decides what a run whose last step is done does, from the
// ship mode, whether it has a scope and whether the change is complete
// (task-scope: Partial runs don't ship, Incomplete whole-change runs). In
// local mode it is always ready. In other modes a complete change ships,
// a partial scoped run is ready and ships nothing, and an incomplete run
// without a scope is blocked.
func shipDecision(mode config.ShipMode, scoped, complete bool) shipOutcome {
	switch {
	case mode == "" || mode == config.ShipLocal:
		return outcomeReady
	case complete:
		return outcomeShip
	case scoped:
		return outcomeReady
	default:
		return outcomeBlocked
	}
}

// scopeReason is the blocked reason for a scope whose tasks are not all
// ticked, or "" when they are.
func scopeReason(unticked, missing []string) string {
	var parts []string
	if len(unticked) > 0 {
		parts = append(parts, "scope tasks not ticked: "+strings.Join(unticked, ", "))
	}
	if len(missing) > 0 {
		parts = append(parts, "scope tasks no longer in tasks.md: "+strings.Join(missing, ", "))
	}
	return strings.Join(parts, "; ")
}
