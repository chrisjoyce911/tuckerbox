package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/edits"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

func gitUserName(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sidecarRel(change string) string {
	return approval.ChangeDir(change) + "/" + approval.SidecarName
}

// withSidecar runs fn holding the sidecar lock of the repository that
// contains dir (change-priority: One writer for the sidecar).
func withSidecar(ctx context.Context, dir string, fn func() error) error {
	repo, err := runs.OpenRepo(ctx, dir)
	if err != nil {
		return err
	}
	return runs.WithSidecarLock(repo.Common, fn)
}

// approve: tuckerbox approve <change>
func (a App) approve(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("approve", "usage: tuckerbox approve <change>")
	}
	change := pos[0]
	ctx := context.Background()
	p, ok := a.editPlace("approve", change)
	if !ok {
		return 1
	}
	var ap approval.Entry
	var invalid *edits.Result
	var pubErr error
	err = edits.WithChangeLock(ctx, p, "tuckerbox approve", func() error {
		res, err := edits.Validator{}.Validate(ctx, p)
		if err != nil {
			return err
		}
		if !res.Valid {
			invalid = res
			return nil
		}
		by := gitUserName(ctx, p.Dir)
		if by == "" {
			return fmt.Errorf("git user.name is not set in %s; set it (git config user.name \"Your Name\") so the approval says who approved", p.Dir)
		}
		hash, err := approval.Hash(approval.DirOf(p.Dir, change))
		if err != nil {
			return err
		}
		ap = approval.Entry{State: approval.EntryApproved, Hash: hash, By: by, At: time.Now().Truncate(time.Second)}
		pubErr = approval.Store{Dir: p.Dir}.Record(ctx, change, ap)
		if errors.Is(pubErr, approval.ErrOffline) {
			return nil
		}
		return pubErr
	})
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	if invalid != nil {
		fmt.Fprint(a.Stderr, invalid.Text())
		return a.errf("approve", "change %s is not valid; fix it before approving. Nothing changed", change)
	}
	fmt.Fprintf(a.Stdout, "approved %s as in %s (by %s at %s, %s)\n", change, p.Describe(), ap.By, ap.At.Format(time.RFC3339), ap.Hash)
	published(a, pubErr)
	return 0
}

// published says where an approve or unapprove now counts.
func published(a App, err error) {
	if errors.Is(err, approval.ErrOffline) {
		fmt.Fprintf(a.Stdout, "not published: %v\n", err)
		return
	}
	fmt.Fprintf(a.Stdout, "published on %s: it counts on every machine now; nothing to commit\n", approval.RefName)
}

// unapprove: tuckerbox unapprove <change>
func (a App) unapprove(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("unapprove", "usage: tuckerbox unapprove <change>")
	}
	change := pos[0]
	p, ok := a.editPlace("unapprove", change)
	if !ok {
		return 1
	}
	var removed bool
	var pubErr error
	ctx := context.Background()
	err = edits.WithChangeLock(ctx, p, "tuckerbox unapprove", func() error {
		st, err := currentApproval(ctx, p.Dir, change)
		if err != nil {
			return err
		}
		if st.Approval == nil {
			return nil
		}
		removed = true
		by := gitUserName(ctx, p.Dir)
		pubErr = approval.Store{Dir: p.Dir}.Record(ctx, change, approval.Entry{State: approval.EntryWithdrawn, By: by, At: time.Now()})
		if errors.Is(pubErr, approval.ErrOffline) {
			return nil
		}
		return pubErr
	})
	if err != nil {
		return a.errf("unapprove", "%v", err)
	}
	if !removed {
		fmt.Fprintf(a.Stdout, "%s has no approval; nothing changed\n", change)
		return 0
	}
	fmt.Fprintf(a.Stdout, "withdrew the approval of %s\n", change)
	published(a, pubErr)
	return 0
}

// currentApproval judges change's approval in dir from the approvals ref
// (synced from the remote) or, without an entry there, its sidecar.
func currentApproval(ctx context.Context, dir, change string) (approval.Status, error) {
	e, err := refEntry(ctx, dir, change)
	if err != nil {
		return approval.Status{}, err
	}
	return approval.JudgeWith(approval.DirOf(dir, change), e)
}

// refEntry fetches the approvals ref and returns change's entry on it, or
// nil. Offline, the local ref is used as it is.
func refEntry(ctx context.Context, dir, change string) (*approval.Entry, error) {
	s := approval.Store{Dir: dir}
	if err := s.Sync(ctx, 0); err != nil {
		return nil, err
	}
	es, err := s.Entries(ctx)
	if err != nil {
		return nil, err
	}
	if e, ok := es[change]; ok {
		return &e, nil
	}
	return nil, nil
}

// runGate is the early approval check in tuckerbox run, for a clear
// message before any process starts. It judges what the run will start
// from: the change's worktree if it has one, else the commit a new worktree
// would be made at (the change's branch if it exists, else the fetched
// base, fetched once). The engine judges again, authoritatively, under the
// change lock (see approvalGate).
type runGate struct {
	a    App
	repo runs.Repo
	cfg  *config.Config
	base string
	err  error
	done bool
}

// check returns the commit a new worktree would start at ("" when the
// change has a worktree or approval is off), or why change may not start.
func (g *runGate) check(ctx context.Context, change string) (string, error) {
	if !g.cfg.RequireApproval {
		return "", nil
	}
	sp, err := g.start(ctx, change)
	if err != nil {
		return "", err
	}
	return sp.Rev, judgeStart(ctx, g.repo, change, sp, true)
}

// start says where change's run would start from: its worktree, or the
// commit a new one would be made at. The base is fetched once.
func (g *runGate) start(ctx context.Context, change string) (runs.StartPoint, error) {
	if !g.done {
		g.done = true
		g.base, g.err = g.repo.Base(ctx, g.cfg.Worktree.Base, func(msg string) {
			fmt.Fprintf(g.a.Stderr, "tuckerbox run: warning: %s\n", msg)
		})
	}
	if g.err != nil {
		return runs.StartPoint{}, g.err
	}
	return g.repo.ResolveStart(ctx, g.cfg.Worktree, change, g.base, nil)
}

// approvalGate is the engine's runs.Engine.Gate when approval is
// required.
func approvalGate(repo runs.Repo) func(context.Context, string, runs.StartPoint) error {
	return func(ctx context.Context, change string, sp runs.StartPoint) error {
		return judgeStart(ctx, repo, change, sp, false)
	}
}

// judgeStart refuses change unless it is approved where sp says the run
// starts from. With note, a refusal on a commit mentions an approval in
// the checkout that is not on it.
func judgeStart(ctx context.Context, repo runs.Repo, change string, sp runs.StartPoint, note bool) error {
	entry, err := refEntry(ctx, repo.Root, change)
	if err != nil {
		return fmt.Errorf("change %s: reading %s: %v", change, approval.RefName, err)
	}
	if sp.Worktree != "" {
		st, err := approval.JudgeWith(approval.DirOf(sp.Worktree, change), entry)
		if err != nil {
			return fmt.Errorf("change %s: reading its approval in %s: %v", change, sp.Worktree, err)
		}
		return gateError(change, st, "in its worktree "+sp.Worktree, "")
	}
	where := "on the base branch " + sp.Ref
	if sp.Branch {
		where = "on its branch " + sp.Ref
	}
	rev, err := approval.NewRev(ctx, repo.Root, sp.Rev, change)
	if err != nil {
		return err
	}
	if !rev.Exists() {
		return fmt.Errorf("change %s is not %s; commit it there first", change, strings.TrimPrefix(where, "on "))
	}
	st, err := approval.JudgeWith(rev, entry)
	if err != nil {
		return fmt.Errorf("change %s: reading its approval %s: %v", change, where, err)
	}
	extra := ""
	if note && st.State != approval.Approved && !st.FromRef {
		if local, err := approval.Judge(approval.DirOf(repo.Root, change)); err == nil && local.Approval != nil &&
			(st.Approval == nil || local.Approval.Hash != st.Approval.Hash || !local.Approval.At.Equal(st.Approval.At)) {
			extra = fmt.Sprintf("; the checkout %s has an approval (%s) that is not on %s: commit and push %s",
				repo.Root, local.State, sp.Ref, sidecarRel(change))
		}
	}
	return gateError(change, st, where, extra)
}

// gateError is nil for an approved change, else says which state stops
// it. where is "in its worktree <path>" or "on the base branch <ref>".
func gateError(change string, st approval.Status, where, note string) error {
	var msg string
	switch st.State {
	case approval.Approved:
		return nil
	case approval.Stale:
		msg = fmt.Sprintf("change %s: approval is stale %s: its text changed since %s approved it at %s; review it and run tuckerbox approve %s",
			change, where, st.Approval.By, st.Approval.At.Format(time.RFC3339), change)
	default:
		msg = fmt.Sprintf("change %s: not approved: there is no approval %s; run tuckerbox approve %s", change, where, change)
		if ref, ok := strings.CutPrefix(where, "on the base branch "); ok {
			msg = fmt.Sprintf("change %s: not approved: the base branch %s has no approval for it; run tuckerbox approve %s", change, ref, change)
		} else if ref, ok := strings.CutPrefix(where, "on its branch "); ok {
			msg = fmt.Sprintf("change %s: not approved: its branch %s has no approval for it; run tuckerbox approve %s, or delete the branch", change, ref, change)
		}
	}
	return &notApprovedError{State: st.State, msg: msg + note}
}

// notApprovedError is a change the run gate refuses.
type notApprovedError struct {
	State approval.State
	msg   string
}

func (e *notApprovedError) Error() string { return e.msg }

// approvalConflicts notes the changes approved both on the approvals ref
// and in their tuckerbox.toml with different hashes (the ref counts).
func approvalConflicts(a App, root string) {
	ctx := context.Background()
	s := approval.Store{Dir: root}
	_ = s.Sync(ctx, time.Minute)
	es, err := s.Entries(ctx)
	if err != nil {
		fmt.Fprintf(a.Stderr, "tuckerbox check: warning: reading %s: %v\n", approval.RefName, err)
		return
	}
	names := make([]string, 0, len(es))
	for c := range es {
		names = append(names, c)
	}
	sort.Strings(names)
	for _, c := range names {
		e := es[c]
		data, err := approval.DirOf(root, c).Read(approval.SidecarName)
		if err != nil {
			continue
		}
		old, err := approval.ParseSidecar(data)
		if err != nil || old == nil || (e.State == approval.EntryApproved && old.Hash == e.Hash) {
			continue
		}
		what := "approved on " + approval.RefName
		if e.State != approval.EntryApproved {
			what = "withdrawn on " + approval.RefName
		}
		fmt.Fprintf(a.Stdout, "note: %s is %s and has a different [approval] in %s; the ref counts\n", c, what, sidecarRel(c))
	}
}
