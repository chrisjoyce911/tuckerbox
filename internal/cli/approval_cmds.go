package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// editPlace is where approve and unapprove act on a change: its worktree
// on this machine if it has one, else the repository checkout.
type editPlace struct {
	Folder string // checkout or worktree root
	// State is the worktree's state directory; "" in the checkout, where
	// no run can be using the files (runs work in their own worktree).
	State statedir.Dir
}

// changeWorktree returns change's worktree root on this machine, or "".
// It looks for a worktree whose run is of change, then for one at the
// configured worktree path.
func changeWorktree(ctx context.Context, repo runs.Repo, wcfg config.Worktree, change string) string {
	wts, err := repo.Worktrees(ctx)
	if err != nil {
		return ""
	}
	for _, wt := range wts {
		if st, err := statedir.Resolve(ctx, wt); err == nil {
			if r, err := runs.ReadRun(st.RunFile()); err == nil && r.Change == change {
				return wt
			}
		}
	}
	want := repo.WorktreePath(wcfg, change)
	for _, wt := range wts {
		if samePlace(wt, want) {
			return wt
		}
	}
	return ""
}

func samePlace(a, b string) bool {
	if ea, err := filepath.EvalSymlinks(a); err == nil {
		a = ea
	}
	if eb, err := filepath.EvalSymlinks(b); err == nil {
		b = eb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// worktreeConfig reads the worktree settings, falling back to the defaults
// when the config can't be loaded: approving needs no workflow.
func (a App) worktreeConfig(root string) config.Worktree {
	env, _ := a.configEnv()
	opts, err := config.Discover(env, root)
	if err != nil {
		return config.Worktree{}
	}
	cfg, _ := config.Load(opts)
	if cfg == nil {
		return config.Worktree{}
	}
	return cfg.Worktree
}

// findEditPlace resolves where change's files are edited. The change edits
// work (change-edits 3.1) shares this rule; swap to its helper once it lands.
func (a App) findEditPlace(ctx context.Context, change string) (runs.Repo, editPlace, error) {
	if err := statedir.CheckID("change", change); err != nil {
		return runs.Repo{}, editPlace{}, err
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return runs.Repo{}, editPlace{}, err
	}
	repo, err := runs.OpenRepo(ctx, wd)
	if err != nil {
		return repo, editPlace{}, err
	}
	p := editPlace{Folder: repo.Root}
	if wt := changeWorktree(ctx, repo, a.worktreeConfig(repo.Root), change); wt != "" {
		p.Folder = wt
		st, err := statedir.Resolve(ctx, wt)
		if err != nil {
			return repo, p, err
		}
		p.State = st
	}
	if fi, err := os.Stat(string(approval.DirOf(p.Folder, change))); err != nil || !fi.IsDir() {
		return repo, p, fmt.Errorf("no change %s in %s", change, p.Folder)
	}
	return repo, p, nil
}

// lockForEdit takes the change's run lock while approve or unapprove
// writes, so no run can start meanwhile, and refuses while a run is queued
// or running, naming its state. With no worktree there is nothing to lock.
//
// This uses the run lock in internal/runs directly until the change edits
// work (change-edits 3.6) adds its shared helper; swap it then.
func lockForEdit(p editPlace, change string) (release func(), err error) {
	if p.State == "" {
		return func() {}, nil
	}
	l, err := runs.TryChangeLock(p.State.RunLock())
	if errors.Is(err, runs.ErrLocked) {
		what := "has an active run"
		if r, rerr := runs.ReadRun(p.State.RunFile()); rerr == nil && r.Change == change {
			what = "is " + string(r.State)
			if r.Step != "" {
				what += " (step " + r.Step + ")"
			}
		}
		return nil, fmt.Errorf("change %s %s in %s; try again once the run stops", change, what, p.Folder)
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = l.Release() }, nil
}

// validateStrict runs openspec validate --strict on change in folder, as
// tuckerbox validate does (change-edits 3.4; swap to it once it lands). It
// returns the output when the change is invalid.
func validateStrict(ctx context.Context, folder, change string) (bool, string, error) {
	c := exec.CommandContext(ctx, "openspec", "validate", change, "--type", "change", "--strict", "--no-interactive")
	c.Dir = folder
	c.Env = append(os.Environ(), "NO_COLOR=1", "OPENSPEC_TELEMETRY=0")
	out, err := c.CombinedOutput()
	if err == nil {
		return true, "", nil
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false, "", fmt.Errorf("running openspec validate %s: %w", change, err)
	}
	return false, strings.TrimSpace(string(out)), nil
}

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
	_, p, err := a.findEditPlace(ctx, change)
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	release, err := lockForEdit(p, change)
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	defer release()
	ok, out, err := validateStrict(ctx, p.Folder, change)
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	if !ok {
		if out != "" {
			fmt.Fprintln(a.Stderr, out)
		}
		return a.errf("approve", "change %s is not valid (openspec validate --strict); fix it before approving. Nothing changed", change)
	}
	by := gitUserName(ctx, p.Folder)
	if by == "" {
		return a.errf("approve", "git user.name is not set in %s; set it (git config user.name \"Your Name\") so the approval says who approved", p.Folder)
	}
	ap, err := approval.Approve(approval.DirOf(p.Folder, change), by, time.Now())
	if err != nil {
		return a.errf("approve", "%v", err)
	}
	fmt.Fprintf(a.Stdout, "approved %s in %s (by %s at %s, %s)\n", change, p.Folder, ap.By, ap.At.Format(time.RFC3339), ap.Hash)
	fmt.Fprintf(a.Stdout, "not committed: commit and push %s; the approval only counts elsewhere, and for a run on a new worktree, once it is on the base branch\n", sidecarRel(change))
	return 0
}

// unapprove: tuckerbox unapprove <change>
func (a App) unapprove(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("unapprove", "usage: tuckerbox unapprove <change>")
	}
	change := pos[0]
	ctx := context.Background()
	_, p, err := a.findEditPlace(ctx, change)
	if err != nil {
		return a.errf("unapprove", "%v", err)
	}
	release, err := lockForEdit(p, change)
	if err != nil {
		return a.errf("unapprove", "%v", err)
	}
	defer release()
	removed, err := approval.Unapprove(approval.DirOf(p.Folder, change))
	if err != nil {
		return a.errf("unapprove", "%v", err)
	}
	if !removed {
		fmt.Fprintf(a.Stdout, "%s has no approval in %s; nothing changed\n", change, p.Folder)
		return 0
	}
	fmt.Fprintf(a.Stdout, "removed the approval of %s in %s\n", change, p.Folder)
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
