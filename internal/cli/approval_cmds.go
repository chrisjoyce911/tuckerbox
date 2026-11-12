package cli

import (
	"context"
	"fmt"
	"os/exec"
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
	var ap approval.Approval
	var invalid *edits.Result
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
		ap, err = approval.Approve(approval.DirOf(p.Dir, change), by, time.Now())
		return err
	})
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	if invalid != nil {
		fmt.Fprint(a.Stderr, invalid.Text())
		return a.errf("approve", "change %s is not valid; fix it before approving. Nothing changed", change)
	}
	fmt.Fprintf(a.Stdout, "approved %s in %s (by %s at %s, %s)\n", change, p.Describe(), ap.By, ap.At.Format(time.RFC3339), ap.Hash)
	fmt.Fprintf(a.Stdout, "not committed: commit and push %s; the approval only counts elsewhere, and for a run on a new worktree, once it is on the branch the run starts from\n", sidecarRel(change))
	return 0
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
	err = edits.WithChangeLock(context.Background(), p, "tuckerbox unapprove", func() error {
		var err error
		removed, err = approval.Unapprove(approval.DirOf(p.Dir, change))
		return err
	})
	if err != nil {
		return a.errf("unapprove", "%v", err)
	}
	if !removed {
		fmt.Fprintf(a.Stdout, "%s has no approval in %s; nothing changed\n", change, p.Describe())
		return 0
	}
	fmt.Fprintf(a.Stdout, "removed the approval of %s in %s\n", change, p.Describe())
	fmt.Fprintf(a.Stdout, "not committed: commit and push %s so other machines see it\n", sidecarRel(change))
	return 0
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
	if !g.done {
		g.done = true
		g.base, g.err = g.repo.Base(ctx, g.cfg.Worktree.Base, func(msg string) {
			fmt.Fprintf(g.a.Stderr, "tuckerbox run: warning: %s\n", msg)
		})
	}
	if g.err != nil {
		return "", g.err
	}
	sp, err := g.repo.ResolveStart(ctx, g.cfg.Worktree, change, g.base, nil)
	if err != nil {
		return "", err
	}
	return sp.Rev, judgeStart(ctx, g.repo, change, sp, true)
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
	if sp.Worktree != "" {
		st, err := approval.Judge(approval.DirOf(sp.Worktree, change))
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
	st, err := approval.Judge(rev)
	if err != nil {
		return fmt.Errorf("change %s: reading its approval %s: %v", change, where, err)
	}
	extra := ""
	if note && st.State != approval.Approved {
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
			msg = fmt.Sprintf("change %s: not approved: the base branch %s has no approval for it; run tuckerbox approve %s, then commit and push it", change, ref, change)
		} else if ref, ok := strings.CutPrefix(where, "on its branch "); ok {
			msg = fmt.Sprintf("change %s: not approved: its branch %s has no approval for it; approve it there (commit the approval on %s) or delete the branch", change, ref, ref)
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
