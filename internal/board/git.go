package board

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/url"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// RepoKey normalizes clone URLs and resolves SSH Host aliases locally through
// ssh -G. It never connects to the remote. If ssh is unavailable, the literal
// host remains the best available identity.
func RepoKey(ctx context.Context, remote string) string {
	return repoKey(ctx, remote, func(ctx context.Context, host string) (string, error) {
		c := exec.CommandContext(ctx, "ssh", "-G", "--", host)
		c.WaitDelay = 500 * time.Millisecond
		c.Stderr = io.Discard
		configureSSH(c)
		out, err := c.Output()
		return string(out), err
	})
}

var sshHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func repoKey(ctx context.Context, remote string, ssh func(context.Context, string) (string, error)) string {
	key := config.NormalizeRemote(remote)
	var host string
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil && (u.Scheme == "ssh" || u.Scheme == "git+ssh") {
			host = u.Hostname()
		}
	} else if i := strings.Index(remote, ":"); i > 1 && !strings.ContainsAny(remote[:i], `/\`) {
		host = remote[:i]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
	}
	if !sshHost.MatchString(host) {
		return key
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := ssh(ctx, host)
	if err != nil {
		return key
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.EqualFold(f[0], "hostname") {
			if _, rest, ok := strings.Cut(key, "/"); ok {
				return strings.ToLower(f[1]) + "/" + rest
			}
		}
	}
	return key
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
