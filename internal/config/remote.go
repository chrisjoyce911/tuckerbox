package config

import (
	"path"

	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
)

// NormalizeRemote returns the shared git remote identity.
func NormalizeRemote(remote string) string { return gitremote.Normalize(remote) }

// remoteAllowed reports whether a normalised remote matches one of the
// allowed patterns. "*" matches any repository, including one without a
// remote; other patterns match with path.Match, so "github.com/owner/*"
// matches every repository of owner.
func remoteAllowed(allowed []string, remote string) bool {
	for _, pat := range allowed {
		if pat == "*" {
			return true
		}
		if remote == "" {
			continue
		}
		if ok, err := path.Match(pat, remote); err == nil && ok {
			return true
		}
	}
	return false
}

func isSCP(remote string) bool { return gitremote.IsSCP(remote) }
