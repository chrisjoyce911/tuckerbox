package team

import (
	"context"
	"fmt"
	"github.com/BurntSushi/toml"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"os/exec"
	"strings"
)

func git(ctx context.Context, root string, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

// Base fetches the exact protected branch without falling back to stale refs.
func Base(ctx context.Context, root string, cfg *config.Config, c forge.TeamClient) (string, string, error) {
	access, err := c.Access(ctx)
	if err != nil {
		return "", "", err
	}
	branch := cfg.Worktree.Base
	if branch == "" {
		branch = access.DefaultBranch
	}
	remote := cfg.Shipping.Remote
	if remote == "" {
		remote = "origin"
	}
	branch = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(branch, "refs/heads/"), "refs/remotes/"), remote+"/")
	if branch == "" {
		return "", "", fmt.Errorf("team approvals need a protected base branch: forge returned no base")
	}
	protected, err := c.ProtectedBase(ctx, branch)
	if err != nil {
		return "", "", fmt.Errorf("team approvals need a protected base branch: %w", err)
	}
	if !protected {
		return "", "", fmt.Errorf("team approvals need a protected base branch: %s allows direct pushes or needs no approving review", branch)
	}
	ref := "refs/remotes/" + remote + "/" + branch
	if _, err = git(ctx, root, "fetch", "--quiet", remote, "+refs/heads/"+branch+":"+ref); err != nil {
		return "", "", err
	}
	rev, err := git(ctx, root, "rev-parse", ref+"^{commit}")
	return rev, branch, err
}

// Judge uses only the sidecar approval and approver list in the fetched base.
// Actual may be a retained branch or an edited worktree. Its own approval is
// deliberately ignored. Resume checks settings, allowing a plan to edit text.
func Judge(ctx context.Context, root, rev, change string, actual approval.Files, resume bool) (approval.Status, error) {
	data, err := git(ctx, root, "show", rev+":"+config.RepoFile)
	if err != nil {
		return approval.Status{}, err
	}
	var policy struct{ Team config.Team }
	if _, err = toml.Decode(data, &policy); err != nil {
		return approval.Status{}, err
	}
	base, err := approval.NewRev(ctx, root, rev, change)
	if err != nil {
		return approval.Status{}, err
	}
	st, err := approval.Judge(base)
	if err != nil {
		return st, err
	}
	if st.Approval == nil {
		return st, fmt.Errorf("change %s: the base branch has no approval", change)
	}
	listed := false
	for _, login := range policy.Team.Approvers {
		listed = listed || login == st.Approval.Login
	}
	if !listed || st.Approval.Login == "" {
		return st, fmt.Errorf("change %s: %s is not an approver on the base branch", change, st.Approval.Login)
	}
	// JudgeWith uses the base's approval hashes over the actual input files.
	a := st.Approval
	entry := &approval.Entry{State: approval.EntryApproved, Hash: a.Hash, SettingsHash: a.SettingsHash, Settings: a.Settings, By: a.By, At: a.At}
	st, err = approval.JudgeWith(actual, entry)
	if err != nil {
		return st, err
	}
	if len(st.SettingsChanged) > 0 {
		return st, fmt.Errorf("change %s: approval on the base branch doesn't cover %s", change, strings.Join(st.SettingsChanged, ", "))
	}
	if !resume && st.State != approval.Approved {
		return st, fmt.Errorf("change %s: files don't match the approval on the base branch", change)
	}
	return st, nil
}
