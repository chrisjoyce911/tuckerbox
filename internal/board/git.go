package board

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
)

// RepoKey returns the shared remote identity, resolving local SSH aliases.
func RepoKey(ctx context.Context, remote string) string { return gitremote.Key(ctx, remote) }

func repoKey(ctx context.Context, remote string, ssh func(context.Context, string) (string, error)) string {
	return gitremote.Resolve(ctx, remote, ssh)
}

func output(ctx context.Context, dir, program string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, program, args...)
	c.Dir = dir
	b, err := c.Output()
	return string(b), err
}

// committed counts staged, unstaged, deleted and untracked hashed files.
// Sidecar title/dependency edits alone do not mark approval uncommitted.
func committed(ctx context.Context, folder string) (bool, error) {
	root, err := output(ctx, folder, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(strings.TrimSpace(root), folder)
	if err != nil {
		return false, err
	}
	rel = filepath.ToSlash(rel)
	status, err := output(ctx, folder, "git", "-C", strings.TrimSpace(root), "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", rel+"/")
	if err != nil {
		return false, err
	}
	// With -z, renames carry a second path. Both paths matter for hashing.
	parts := strings.Split(status, "\x00")
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		paths := []string{part[3:]}
		if strings.ContainsAny(part[:2], "RC") && i+1 < len(parts) {
			i++
			paths = append(paths, parts[i])
		}
		for _, p := range paths {
			name := strings.TrimPrefix(p, rel+"/")
			switch name {
			case "proposal.md", "design.md", "model.md", "tasks.md":
				return false, nil
			}
			if approval.IsSpec(name) {
				return false, nil
			}
		}
	}
	current, err := approval.Dir(folder).Read(approval.SidecarName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	a, err := approval.ParseSidecar(current)
	if err != nil {
		return false, err
	}
	old, err := output(ctx, folder, "git", "show", "HEAD:"+rel+"/"+approval.SidecarName)
	if err != nil {
		// A missing blob means no committed approval, including an unborn HEAD.
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		old = ""
	}
	b, err := approval.ParseSidecar([]byte(old))
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(a, b) {
		return false, nil
	}
	staged, err := output(ctx, folder, "git", "show", ":"+rel+"/"+approval.SidecarName)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		return b == nil, nil
	}
	indexApproval, err := approval.ParseSidecar([]byte(staged))
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(indexApproval, b), nil
}
