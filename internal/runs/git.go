package runs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// git runs git in dir and returns its trimmed standard output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// Repo is the git repository a run belongs to.
type Repo struct {
	// Root is the checkout the command ran in; its config is used.
	Root string
	// Main is the main worktree's root: worktree path patterns and {repo}
	// are relative to it.
	Main string
	// Common is the Tuckerbox directory in the git common directory, shared
	// by all worktrees (slots, queue).
	Common string
}

// OpenRepo finds the repository that contains dir.
func OpenRepo(ctx context.Context, dir string) (Repo, error) {
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("%s is not in a git repository: %w", dir, err)
	}
	root = filepath.Clean(filepath.FromSlash(root))
	common, err := statedir.ResolveCommon(ctx, root)
	if err != nil {
		return Repo{}, err
	}
	main := root
	// The common dir of a non-bare repository is <main worktree>/.git.
	if gd := filepath.Dir(common); filepath.Base(gd) == ".git" {
		main = filepath.Dir(gd)
	}
	return Repo{Root: root, Main: main, Common: common}, nil
}

// Checkout is the repository checkout on this machine, whose change folders
// hold the priorities (change-priority: Where priority is written): the
// main worktree, never a change's worktree.
func (r Repo) Checkout() string { return r.Main }

// Worktrees lists the repository's worktree roots, the main one first.
func (r Repo) Worktrees(ctx context.Context) ([]string, error) {
	out, err := git(ctx, r.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var wts []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "worktree "); ok {
			wts = append(wts, filepath.Clean(filepath.FromSlash(p)))
		}
	}
	return wts, nil
}

// expand fills {change} and {repo} in a worktree or branch pattern.
func (r Repo) expand(pattern, change string) string {
	s := strings.ReplaceAll(pattern, "{change}", change)
	return strings.ReplaceAll(s, "{repo}", filepath.Base(r.Main))
}

// WorktreePath is where change's worktree goes under cfg.
func (r Repo) WorktreePath(cfg config.Worktree, change string) string {
	pattern := cfg.Path
	if pattern == "" {
		pattern = config.DefaultWorktreePath
	}
	p := filepath.FromSlash(r.expand(pattern, change))
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.Main, p)
	}
	return filepath.Clean(p)
}

// BranchName is change's branch under cfg.
func (r Repo) BranchName(cfg config.Worktree, change string) string {
	pattern := cfg.Branch
	if pattern == "" {
		pattern = config.DefaultBranch
	}
	return r.expand(pattern, change)
}

// remote returns the remote the base branch comes from: origin, or the only
// remote; "" if there is none.
func (r Repo) remote(ctx context.Context) string {
	out, err := git(ctx, r.Root, "remote")
	if err != nil {
		return ""
	}
	names := strings.Fields(out)
	for _, n := range names {
		if n == "origin" {
			return n
		}
	}
	if len(names) == 1 {
		return names[0]
	}
	return ""
}

func (r Repo) refExists(ctx context.Context, ref string) bool {
	_, err := git(ctx, r.Root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// Base resolves the ref new branches start from. A configured base is used
// as is, preferring the remote's copy after a fetch. Otherwise it is the
// remote's default branch after a fetch, or the local default branch when
// there is no remote. warn receives a failed fetch, which is not fatal.
func (r Repo) Base(ctx context.Context, configured string, warn func(string)) (string, error) {
	remote := r.remote(ctx)
	if remote != "" {
		if _, err := git(ctx, r.Root, "fetch", "--quiet", remote); err != nil && warn != nil {
			warn(fmt.Sprintf("fetching %s failed; using what is known locally: %v", remote, err))
		}
	}
	if configured != "" {
		if remote != "" && r.refExists(ctx, remote+"/"+configured) {
			return remote + "/" + configured, nil
		}
		if r.refExists(ctx, configured) {
			return configured, nil
		}
		return "", fmt.Errorf("base branch %q does not exist", configured)
	}
	if remote != "" {
		if head, err := git(ctx, r.Root, "symbolic-ref", "--short", "refs/remotes/"+remote+"/HEAD"); err == nil && r.refExists(ctx, head) {
			return head, nil
		}
		for _, b := range []string{"main", "master"} {
			if r.refExists(ctx, remote+"/"+b) {
				return remote + "/" + b, nil
			}
		}
	}
	var candidates []string
	if def, err := git(ctx, r.Root, "config", "--get", "init.defaultBranch"); err == nil && def != "" {
		candidates = append(candidates, def)
	}
	candidates = append(candidates, "main", "master")
	for _, b := range candidates {
		if r.refExists(ctx, "refs/heads/"+b) {
			return b, nil
		}
	}
	// A repository whose only branch has another name: the main worktree's
	// branch.
	if head, err := git(ctx, r.Main, "symbolic-ref", "--short", "HEAD"); err == nil && r.refExists(ctx, head) {
		return head, nil
	}
	return "", errors.New("cannot find the default branch; set [worktree] base")
}

// Workspace is a change's worktree.
type Workspace struct {
	Path, Branch, Base string
	State              statedir.Dir
	// Created is true when this call made the worktree.
	Created bool
}

// EnsureWorktree returns change's worktree, making it (and its branch from
// the base) when it does not exist. An existing worktree is reused as it
// is: nothing in it is reset, cleaned or stashed. Creation is serialised by
// a lock in the common directory, so two processes never both make it.
func (r Repo) EnsureWorktree(ctx context.Context, cfg config.Worktree, change string, warn func(string)) (Workspace, error) {
	return r.EnsureWorktreeFrom(ctx, cfg, change, nil, warn)
}

// StartPoint is what a change's worktree comes from: its existing
// worktree, or the commit a new one would be made at.
type StartPoint struct {
	// Worktree is the change's existing worktree; then Rev is "".
	Worktree string
	// Rev is the commit a new worktree is made at, and Ref what it was
	// resolved from: the change's own branch when Branch is set, else the
	// base branch.
	Rev, Ref string
	Branch   bool
}

// ResolveStart says where change's worktree would come from, as
// EnsureWorktree decides it: an existing worktree, the change's existing
// branch, or base (resolved with Base, which fetches, when "").
func (r Repo) ResolveStart(ctx context.Context, cfg config.Worktree, change, base string, warn func(string)) (StartPoint, error) {
	path, branch := r.WorktreePath(cfg, change), r.BranchName(cfg, change)
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return StartPoint{}, err
	}
	for _, w := range wts {
		if samePath(w, path) {
			return StartPoint{Worktree: w}, nil
		}
	}
	if r.refExists(ctx, "refs/heads/"+branch) {
		rev, err := git(ctx, r.Root, "rev-parse", "refs/heads/"+branch+"^{commit}")
		return StartPoint{Rev: rev, Ref: branch, Branch: true}, err
	}
	if base == "" {
		if base, err = r.Base(ctx, cfg.Base, warn); err != nil {
			return StartPoint{}, err
		}
	}
	rev, err := git(ctx, r.Root, "rev-parse", base+"^{commit}")
	return StartPoint{Rev: rev, Ref: base}, err
}

// EnsureWorktreeFrom is EnsureWorktree with a new worktree pinned to
// pin.Rev, when pin is not nil: made at that commit from the base, or
// from the change's branch only while the branch is still at it. An
// existing worktree is reused as it is.
func (r Repo) EnsureWorktreeFrom(ctx context.Context, cfg config.Worktree, change string, pin *StartPoint, warn func(string)) (Workspace, error) {
	ws := Workspace{Path: r.WorktreePath(cfg, change), Branch: r.BranchName(cfg, change)}
	if err := os.MkdirAll(r.Common, 0o755); err != nil {
		return ws, err
	}
	l := flock.New(filepath.Join(r.Common, "worktree.lock"))
	lctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if ok, err := l.TryLockContext(lctx, 50*time.Millisecond); err != nil || !ok {
		return ws, fmt.Errorf("waiting for the worktree lock: %v", err)
	}
	defer l.Unlock()

	wts, err := r.Worktrees(ctx)
	if err != nil {
		return ws, err
	}
	exists := false
	for _, w := range wts {
		if samePath(w, ws.Path) {
			exists = true
			ws.Path = w
		}
	}
	switch {
	case exists:
		if b, err := git(ctx, ws.Path, "symbolic-ref", "--short", "HEAD"); err == nil {
			ws.Branch = b
		}
	case r.refExists(ctx, "refs/heads/"+ws.Branch):
		if _, err := os.Stat(ws.Path); err == nil {
			return ws, fmt.Errorf("%s exists but is not a worktree of this repository", ws.Path)
		}
		if pin != nil {
			if !pin.Branch {
				return ws, fmt.Errorf("branch %s appeared since change %s was checked; run it again", ws.Branch, change)
			}
			if rev, err := git(ctx, r.Root, "rev-parse", "refs/heads/"+ws.Branch+"^{commit}"); err != nil || rev != pin.Rev {
				return ws, fmt.Errorf("branch %s moved since change %s was checked; run it again", ws.Branch, change)
			}
		}
		if _, err := git(ctx, r.Root, "worktree", "add", ws.Path, ws.Branch); err != nil {
			return ws, err
		}
		ws.Created = true
		if pin != nil {
			if head, err := git(ctx, ws.Path, "rev-parse", "HEAD"); err != nil || head != pin.Rev {
				return ws, fmt.Errorf("branch %s moved while its worktree was made; check %s and run it again", ws.Branch, ws.Path)
			}
		}
	default:
		if _, err := os.Stat(ws.Path); err == nil {
			return ws, fmt.Errorf("%s exists but is not a worktree of this repository", ws.Path)
		}
		var base string
		switch {
		case pin != nil && pin.Branch:
			return ws, fmt.Errorf("branch %s of change %s disappeared since it was checked; run it again", ws.Branch, change)
		case pin != nil:
			// The commit that was checked, not wherever the base is now.
			base, ws.Base = pin.Rev, pin.Ref
		default:
			var err error
			if base, err = r.Base(ctx, cfg.Base, warn); err != nil {
				return ws, err
			}
			ws.Base = base
		}
		// The change must be on the base, or its agents can't read it.
		if out, err := git(ctx, r.Root, "ls-tree", "--name-only", base, "openspec/changes/"+change+"/"); err != nil || out == "" {
			return ws, fmt.Errorf("change %s is not on the base branch %s; commit it there first", change, base)
		}
		if _, err := git(ctx, r.Root, "worktree", "add", "--no-track", "-b", ws.Branch, ws.Path, base); err != nil {
			return ws, err
		}
		ws.Created = true
	}
	st, err := statedir.Resolve(ctx, ws.Path)
	if err != nil {
		return ws, err
	}
	ws.State = st
	return ws, nil
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ea, err1 := filepath.EvalSymlinks(a)
	eb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil && filepath.Clean(ea) == filepath.Clean(eb) {
		return true
	}
	return sameFile(a, b)
}

// CommitAll commits everything in the worktree, if anything changed, and
// reports whether it made a commit.
func CommitAll(ctx context.Context, worktree, message string) (bool, error) {
	out, err := git(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}
	if _, err := git(ctx, worktree, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := git(ctx, worktree, "commit", "--quiet", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}
