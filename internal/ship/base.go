package ship

import (
	"context"
	"fmt"
	"strings"
)

// DeleteMerged deletes only the branch tip that was observed merged. The
// deletion's own lease protects a person who pushes between read and delete.
func (g Git) DeleteMerged(ctx context.Context, merged string) (bool, error) {
	if merged == "" {
		return false, fmt.Errorf("merged head is required")
	}
	if err := g.validate(ctx); err != nil {
		return false, err
	}
	if err := g.protectDefault(ctx); err != nil {
		return false, err
	}
	tip, err := g.Tip(ctx)
	if err != nil {
		return false, err
	}
	if tip == "" || tip != merged {
		return false, nil
	}
	ref := "refs/heads/" + g.Branch
	if _, err := g.run(ctx, "push", "--force-with-lease="+ref+":"+merged, "--", g.remote(), ":"+ref); err != nil {
		return false, err
	}
	return true, nil
}

// UpdateBase uses a merge, never a rebase. It leaves a clean pre-merge worktree
// intact when conflicts require a person, and returns the conflicting files.
func (g Git) UpdateBase(ctx context.Context) (string, error) {
	branch, err := g.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != g.Branch {
		return "", fmt.Errorf("worktree is not on the shipping branch")
	}
	if err := g.validate(ctx); err != nil {
		return "", err
	}
	status, err := g.run(ctx, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "", fmt.Errorf("worktree is not clean; cannot merge base")
	}
	base := strings.TrimPrefix(g.Base, "refs/heads/")
	base = strings.TrimPrefix(base, "refs/remotes/")
	base = strings.TrimPrefix(base, g.remote()+"/")
	if _, err := g.run(ctx, "check-ref-format", "--branch", base); err != nil {
		return "", fmt.Errorf("invalid base branch")
	}
	if _, err := g.run(ctx, "fetch", "--no-tags", "--", g.remote(), "refs/heads/"+base); err != nil {
		return "", err
	}
	fetched, err := g.run(ctx, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if _, err := g.run(ctx, "merge", "--no-edit", fetched); err != nil {
		files, _ := g.run(ctx, "diff", "--name-only", "--diff-filter=U")
		if _, abortErr := g.run(ctx, "merge", "--abort"); abortErr != nil {
			return "", fmt.Errorf("base merge failed; abort also failed: %w", abortErr)
		}
		return "", fmt.Errorf("conflict with the base: %s", strings.ReplaceAll(files, "\n", ", "))
	}
	return g.Head(ctx)
}
