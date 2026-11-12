package runs

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/scope"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// LocalRun is a change's run in one of the repository's worktrees on this
// machine, as tuckerbox autopilot reads it.
type LocalRun struct {
	// Run is run.json as written, not the effective state: a conditional
	// request compares against these State and Since values.
	Run *Run
	// Active reports that a process holds the change lock (a run, or an
	// edit).
	Active   bool
	State    statedir.Dir
	Worktree string
}

// Crashed reports that the run's process died without recording a final
// state: run.json says queued or running while nobody holds the lock.
func (l LocalRun) Crashed() bool {
	return !l.Active && (l.Run.State == Queued || l.Run.State == Running)
}

// Busy reports that the run takes one of the repository's max_parallel
// places: its process holds the lock while it is queued or running.
func (l LocalRun) Busy() bool {
	return l.Active && (l.Run.State == Queued || l.Run.State == Running)
}

// LocalRuns reads the run of every worktree of repo, by change.
func LocalRuns(ctx context.Context, repo Repo) (map[string]LocalRun, error) {
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]LocalRun{}
	for _, wt := range wts {
		st, err := statedir.Resolve(ctx, wt)
		if err != nil {
			continue
		}
		r, err := ReadRun(st.RunFile())
		if err != nil {
			continue
		}
		out[r.Change] = LocalRun{Run: r, Active: Active(st.RunLock()), State: st, Worktree: wt}
	}
	return out, nil
}

// DependencyLookup returns the lookup the engine judges dependencies with,
// read once now: a ready run counts only without a scope or when complete,
// and outside ship = "local" only an archive counts.
func DependencyLookup(ctx context.Context, repo Repo, cfg *config.Config) deps.Lookup {
	l := newDepLookup(repo)
	l.archiveOnly = cfg.Ship != "" && cfg.Ship != config.ShipLocal
	_ = l.refresh(ctx)
	return l.lookup
}

// ActiveAt lists the active changes at rev: the folders in openspec/changes
// other than archive, sorted.
func ActiveAt(ctx context.Context, repo Repo, rev string) ([]string, error) {
	out, err := git(ctx, repo.Root, "ls-tree", "-d", "--name-only", rev+":openspec/changes")
	if err != nil {
		if strings.Contains(err.Error(), "Not a valid object name") || strings.Contains(err.Error(), "not a tree object") {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		l = filepath.Base(strings.TrimSpace(l))
		if l != "" && l != "archive" && !strings.HasPrefix(l, ".") && statedir.CheckID("change", l) == nil {
			ids = append(ids, l)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ArchivedAt lists the changes archived at rev, by id.
func ArchivedAt(ctx context.Context, repo Repo, rev string) (map[string]bool, error) {
	out := map[string]bool{}
	names, err := git(ctx, repo.Root, "ls-tree", "-d", "--name-only", rev+":openspec/changes/archive")
	if err != nil {
		return out, nil // no archive folder
	}
	for _, l := range strings.Split(names, "\n") {
		name := filepath.Base(strings.TrimSpace(l))
		if m := archivedName.FindStringSubmatch(name); m != nil {
			out[m[1]] = true
		} else if name != "" {
			out[name] = true
		}
	}
	return out, nil
}

var archivedName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-(.+)$`)

// ReadTasks parses the tasks.md at p; a missing file has no tasks.
func ReadTasks(p string) ([]scope.Task, error) { return readTasks(p) }

// ShowAt returns the file at path (slash-separated, from the repository
// root) in rev, or ok false when rev doesn't have it.
func ShowAt(ctx context.Context, repo Repo, rev, path string) (data string, ok bool) {
	out, err := git(ctx, repo.Root, "cat-file", "blob", rev+":"+path)
	if err != nil {
		return "", false
	}
	return out, true
}

// RevOf resolves ref to a commit.
func RevOf(ctx context.Context, repo Repo, ref string) (string, error) {
	return git(ctx, repo.Root, "rev-parse", ref+"^{commit}")
}
