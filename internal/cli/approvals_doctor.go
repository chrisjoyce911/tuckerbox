package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// approvalsDoctorLine says whether this repository's approvals can be
// published (approval-publish), or "" outside a Tuckerbox repository.
func (a App) approvalsDoctorLine() string {
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return ""
	}
	opts, err := config.Discover(env, wd)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := approval.Store{Dir: opts.RepoRoot}
	if !s.HasRemote(ctx) {
		return "approvals: no remote " + approval.Remote + "; approvals stay on this machine"
	}
	if err := s.CheckRemote(ctx); err != nil {
		return "approvals: " + err.Error()
	}
	line := fmt.Sprintf("approvals: %s on %s can be fetched and pushed", approval.RefName, approval.Remote)
	if un, err := s.Unpublished(ctx); err == nil && len(un) > 0 {
		line += fmt.Sprintf("; %d not published yet", len(un))
	}
	return line
}
