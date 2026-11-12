package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

const priorityUsage = "usage: tuckerbox priority [--json] | tuckerbox priority <change> <n|up|down|top>"

// PriorityVersion is the version of tuckerbox priority --json.
const PriorityVersion = 1

type priorityEntry struct {
	Position int    `json:"position"`
	Change   string `json:"change"`
	Priority *int64 `json:"priority"`
}

type priorityDoc struct {
	Version  int             `json:"version"`
	Checkout string          `json:"checkout"`
	Changes  []priorityEntry `json:"changes"`
}

// priority: tuckerbox priority [--json] lists the active changes in
// priority order; tuckerbox priority <change> <n|up|down|top> moves one.
// Both work in the repository checkout (change-priority: Where priority is
// written), never in a change's worktree, and never commit.
func (a App) priority(args []string) int {
	var asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, nil)
	if err != nil {
		return a.usageErr("priority", err.Error())
	}
	switch {
	case len(pos) == 0:
	case len(pos) == 2 && !asJSON:
	default:
		return a.usageErr("priority", priorityUsage)
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("priority", "%v", err)
	}
	ctx := context.Background()
	repo, err := runs.OpenRepo(ctx, wd)
	if err != nil {
		return a.errf("priority", "%v", err)
	}
	if len(pos) == 0 {
		return a.priorityList(repo.Checkout(), asJSON)
	}
	return a.priorityMove(repo, pos[0], pos[1])
}

func (a App) priorityList(checkout string, asJSON bool) int {
	cs, err := priority.Load(checkout)
	if err != nil {
		return a.errf("priority", "%v", err)
	}
	doc := priorityDoc{Version: PriorityVersion, Checkout: checkout, Changes: []priorityEntry{}}
	for i, c := range priority.Sort(cs) {
		e := priorityEntry{Position: i + 1, Change: c.ID}
		if c.Ranked() {
			v := c.Priority
			e.Priority = &v
		}
		doc.Changes = append(doc.Changes, e)
	}
	if asJSON {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return a.errf("priority", "%v", err)
		}
		a.Stdout.Write(append(data, '\n'))
		return 0
	}
	if len(doc.Changes) == 0 {
		fmt.Fprintln(a.Stdout, "no active changes")
		return 0
	}
	rows := [][]string{{"#", "PRIORITY", "CHANGE"}}
	for _, e := range doc.Changes {
		v := "-"
		if e.Priority != nil {
			v = strconv.FormatInt(*e.Priority, 10)
		}
		rows = append(rows, []string{strconv.Itoa(e.Position), v, e.Change})
	}
	writeTable(a.Stdout, rows)
	return 0
}

func (a App) priorityMove(repo runs.Repo, change, where string) int {
	checkout := repo.Checkout()
	if err := statedir.CheckID("change", change); err != nil {
		return a.errf("priority", "%v; nothing changed", err)
	}
	t, err := priority.ParseTarget(where)
	if err != nil {
		return a.usageErr("priority", err.Error()+"\n"+priorityUsage)
	}
	if !deps.IsActive(checkout, change) {
		if deps.IsArchived(checkout, change) {
			return a.errf("priority", "change %s is archived; only active changes have a priority. Nothing changed", change)
		}
		return a.errf("priority", "no active change %s in the repository checkout %s (no folder openspec/changes/%s); if it is new, pull first. Nothing changed", change, checkout, change)
	}
	var m priority.Move
	var n int
	// Under the sidecar lock for every file of the move, so no admission
	// and no other sidecar writer sees it half done.
	err = runs.WithSidecarLock(repo.Common, func() error {
		cs, err := priority.Load(checkout)
		if err != nil {
			return err
		}
		n = len(cs)
		m, err = priority.Plan(cs, change, t)
		if err != nil {
			return err
		}
		// Every new file first, so a file that can't be rewritten stops
		// the move before anything is written.
		out := map[string][]byte{}
		for id, v := range m.Values {
			p := priority.File(checkout, id)
			old, err := os.ReadFile(p)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			data, err := priority.SetValue(old, v)
			if err != nil {
				return fmt.Errorf("%s: %v", priority.RelFile(id), err)
			}
			out[id] = data
		}
		for _, id := range sortedKeys(out) {
			if err := approval.WriteSidecar(priority.File(checkout, id), out[id]); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, priority.ErrUnknown) {
		return a.errf("priority", "no active change %s in the repository checkout %s; nothing changed", change, checkout)
	}
	if err != nil {
		return a.errf("priority", "%v; nothing changed", err)
	}
	if len(m.Values) == 0 {
		fmt.Fprintf(a.Stdout, "%s is already at position %d of %d; nothing changed\n", change, m.From, n)
		return 0
	}
	fmt.Fprintf(a.Stdout, "moved %s from position %d to %d of %d (priority %d) in the repository checkout %s\n",
		change, m.From, m.To, n, m.Values[change], checkout)
	ids := sortedKeys(m.Values)
	if len(ids) > 1 {
		what := "gave priorities to"
		if m.Renumbered {
			what = "no gap was left, so renumbered"
		}
		var parts []string
		for _, id := range m.Order {
			if v, ok := m.Values[id]; ok && id != change {
				parts = append(parts, fmt.Sprintf("%s %d", id, v))
			}
		}
		fmt.Fprintf(a.Stdout, "%s %s\n", what, strings.Join(parts, ", "))
	}
	files := make([]string, len(ids))
	for i, id := range ids {
		files[i] = priority.RelFile(id)
	}
	fmt.Fprintf(a.Stdout, "not committed: queued runs on this machine follow the new order now; commit and push %s so other machines see it\n", strings.Join(files, " "))
	return 0
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
