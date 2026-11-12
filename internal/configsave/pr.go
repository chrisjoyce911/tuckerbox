package configsave

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// PullRequest edits only a temporary checkout of the fetched base. It never
// merges the resulting request, regardless of the repository's ship mode.
func PullRequest(ctx context.Context, req Request, remote string, client forge.Client, mask func(string) string, result *Result) error {
	if mask == nil {
		mask = func(s string) string { return s }
	}
	root := req.Options.RepoRoot
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git %s: %s", args[0], mask(strings.TrimSpace(stderr.String())))
		}
		return strings.TrimSpace(string(out)), nil
	}
	if remote == "" || strings.HasPrefix(remote, "-") || strings.ContainsAny(remote, " \r\n\t") {
		return fmt.Errorf("invalid config save remote")
	}
	return WithFileLock(ctx, req.StateDir, req.Path, func() error {
		access, err := client.Access(ctx)
		if err != nil {
			return err
		}
		if !access.Push {
			return fmt.Errorf("forge token cannot push to this repository")
		}
		base := req.Base
		if base == "" {
			base = access.DefaultBranch
		}
		base = strings.TrimPrefix(base, "refs/heads/")
		base = strings.TrimPrefix(base, "refs/remotes/")
		base = strings.TrimPrefix(base, remote+"/")
		if base == "" {
			return fmt.Errorf("forge returned no default branch")
		}
		if _, err = git(root, "check-ref-format", "refs/heads/"+base); err != nil {
			return fmt.Errorf("invalid forge default branch")
		}
		// Resolve the fetched commit by an explicit private ref; FETCH_HEAD is
		// shared with unrelated git operations and is not a concurrency fence.
		var nonce [8]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		tag := hex.EncodeToString(nonce[:])
		fetchRef := "refs/tuckerbox/config-save/" + tag
		if _, err = git(root, "fetch", "--no-tags", "--", remote, "refs/heads/"+base+":"+fetchRef); err != nil {
			return err
		}
		defer git(root, "update-ref", "-d", fetchRef)
		commit, err := git(root, "rev-parse", fetchRef+"^{commit}")
		if err != nil {
			return err
		}
		parent, err := os.MkdirTemp("", "tuckerbox-config-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(parent)
		wt := filepath.Join(parent, "checkout")
		if _, err = git(root, "worktree", "add", "--detach", "--", wt, commit); err != nil {
			return err
		}
		// This tree belongs exclusively to this save, including on failure.
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			exec.CommandContext(cleanupCtx, "git", "-C", root, "worktree", "remove", "--force", "--", wt).Run()
		}()
		path := filepath.Join(wt, config.RepoFile)
		before, err := Read(path)
		if err != nil {
			return err
		}
		if err = Match(before, req.IfMatch); err != nil {
			return err
		}
		after, err := Rewrite(before, req.Edits)
		if err != nil {
			return err
		}
		opts := req.Options
		opts.RepoRoot = wt
		if err = Check(opts, before, after, false, result); err != nil {
			return err
		}
		if req.DryRun {
			result.OK = true
			result.Message = "checked against the fetched base; would open a pull request"
			return nil
		}
		if bytes.Equal(before, after) {
			result.OK = true
			result.Message = "configuration already has these values; no pull request needed"
			return nil
		}
		keys := make([]string, len(req.Edits))
		for i, e := range req.Edits {
			keys[i] = PathString(e.Path)
		}
		payload, _ := json.Marshal(req.Edits)
		branch := "tuckerbox/config-" + time.Now().UTC().Format("20060102T150405Z") + "-" + Revision(payload)[:8] + "-" + tag[:8]
		if branch == base || branch == access.DefaultBranch {
			return fmt.Errorf("refusing to push the base branch")
		}
		if _, err = git(wt, "switch", "-c", branch); err != nil {
			return err
		}
		if err = AtomicWrite(path, after, 0644); err != nil {
			return err
		}
		title := "Configure Tuckerbox: " + strings.Join(keys, ", ")
		if _, err = git(wt, "add", "--", filepath.ToSlash(config.RepoFile)); err != nil {
			return err
		}
		if _, err = git(wt, "commit", "-m", title, "--", filepath.ToSlash(config.RepoFile)); err != nil {
			return err
		}
		existing, err := git(wt, "ls-remote", "--refs", "--", remote, "refs/heads/"+branch)
		if err != nil {
			return err
		}
		if existing != "" {
			return fmt.Errorf("config save branch already exists; refusing to update it")
		}
		// A fresh random branch, explicit refspec and --no-force: no base writes,
		// tags, implicit matching pushes, leases, or forced updates.
		if _, err = git(wt, "push", "--no-force", "--", remote, "refs/heads/"+branch+":refs/heads/"+branch); err != nil {
			return err
		}
		mr, err := client.OpenMR(ctx, forge.OpenRequest{Title: title, Body: "Configuration settings change. Review and merge through this repository's normal review process. Tuckerbox does not merge config pull requests.", Branch: branch, Base: base})
		if err != nil {
			return fmt.Errorf("branch %s was pushed, but opening the pull request failed: %w", branch, err)
		}
		if mr == nil || mr.URL == "" {
			return fmt.Errorf("branch %s was pushed, but the forge returned no pull request link", branch)
		}
		result.OK = true
		result.Link = mr.URL
		result.Message = "opened config pull request; checkout unchanged: " + mr.URL
		return nil
	})
}
