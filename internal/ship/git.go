// Package ship implements the fenced git and forge operations used by shipping.
package ship

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Git struct{ Worktree, Remote, Branch, Base, DefaultBranch string }

func (g Git) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", g.Worktree}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
func (g Git) Head(ctx context.Context) (string, error) {
	return g.run(ctx, "rev-parse", "refs/heads/"+g.Branch+"^{commit}")
}
func (g Git) Tip(ctx context.Context) (string, error) {
	if err := g.validate(ctx); err != nil {
		return "", err
	}
	out, err := g.run(ctx, "ls-remote", "--refs", "--", g.remote(), "refs/heads/"+g.Branch)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", nil
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "refs/heads/"+g.Branch {
		return "", fmt.Errorf("invalid remote branch response")
	}
	return fields[0], nil
}
func (g Git) remote() string {
	if g.Remote == "" {
		return "origin"
	}
	return g.Remote
}
func (g Git) validate(ctx context.Context) error {
	if strings.HasPrefix(g.remote(), "-") || strings.ContainsAny(g.remote(), " \t\r\n") {
		return fmt.Errorf("invalid shipping remote")
	}
	if _, err := g.run(ctx, "check-ref-format", "--branch", g.Branch); err != nil {
		return fmt.Errorf("invalid shipping branch")
	}
	if strings.HasPrefix(g.Branch, "-") || g.Branch == "HEAD" {
		return fmt.Errorf("invalid shipping branch")
	}
	base := strings.TrimPrefix(g.Base, "refs/heads/")
	base = strings.TrimPrefix(base, "refs/remotes/")
	base = strings.TrimPrefix(base, g.remote()+"/")
	if g.Branch == base || g.Branch == g.DefaultBranch {
		return fmt.Errorf("will not push the base branch or forge default branch")
	}
	return nil
}

// Push uses an explicit compare-and-swap even on first creation (empty lease).
// It never consults tracking refs: a background fetch cannot weaken its fence.
func (g Git) Push(ctx context.Context, lease string) (string, error) {
	if err := g.validate(ctx); err != nil {
		return "", err
	}
	branch, err := g.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != g.Branch {
		return "", fmt.Errorf("worktree is not on the shipping branch")
	}
	if err := g.protectDefault(ctx); err != nil {
		return "", err
	}
	local, err := g.Head(ctx)
	if err != nil {
		return "", err
	}
	if lease != "" {
		// Once a branch is shipping, fixes append; never drop adopted human work.
		if _, err := g.run(ctx, "merge-base", "--is-ancestor", lease, local); err != nil {
			return "", fmt.Errorf("shipping head does not contain the previous push")
		}
	}
	ref := "refs/heads/" + g.Branch
	if _, err := g.run(ctx, "push", "--force-with-lease="+ref+":"+lease, "--", g.remote(), ref+":"+ref); err != nil {
		return "", fmt.Errorf("branch changed on the forge: %w", err)
	}
	return local, nil
}

// Adopt fast-forwards a clean worktree onto commits added after the last push.
// The new lease must be saved before any later fix may push.
func (g Git) Adopt(ctx context.Context, lease string) (string, error) {
	if err := g.validate(ctx); err != nil {
		return "", err
	}
	branch, err := g.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != g.Branch {
		return "", fmt.Errorf("worktree is not on the shipping branch")
	}
	status, err := g.run(ctx, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fmt.Errorf("worktree is not clean; cannot adopt remote commits")
	}
	if lease == "" {
		return "", fmt.Errorf("remote branch exists")
	}
	if _, err := g.run(ctx, "fetch", "--no-tags", "--", g.remote(), "refs/heads/"+g.Branch); err != nil {
		return "", err
	}
	tip, err := g.run(ctx, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if _, err := g.run(ctx, "merge-base", "--is-ancestor", lease, tip); err != nil {
		return "", fmt.Errorf("branch changed on the forge: remote does not contain the last push")
	}
	if _, err := g.run(ctx, "merge", "--ff-only", tip); err != nil {
		return "", fmt.Errorf("cannot fast-forward worktree onto remote commits: %w", err)
	}
	return tip, nil
}

func (g Git) protectDefault(ctx context.Context) error {
	// Verify the actual push remote's default as well as the API's default.
	head, err := g.run(ctx, "ls-remote", "--symref", "--", g.remote(), "HEAD")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(head, "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "ref:" && f[1] == "refs/heads/"+g.Branch {
			return fmt.Errorf("will not push the forge default branch")
		}
	}

	return nil
}
