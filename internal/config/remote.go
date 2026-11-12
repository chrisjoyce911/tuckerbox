package config

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// NormalizeRemote turns a git remote URL into a plain host/path form, so the
// same repository matches however it was cloned. For example
// "git@github.com:owner/repo.git", "https://github.com/owner/repo" and
// "ssh://git@github.com/owner/repo.git" all become "github.com/owner/repo".
// The host is lower-cased; the user, port, ".git" suffix and trailing
// slashes are dropped. A local path is returned cleaned, with forward slashes.
func NormalizeRemote(remote string) string {
	r := strings.TrimSpace(remote)
	if r == "" {
		return ""
	}
	var host, p string
	switch {
	case strings.Contains(r, "://"):
		u, err := url.Parse(r)
		if err != nil {
			return strings.TrimSuffix(strings.TrimRight(r, "/"), ".git")
		}
		if u.Scheme == "file" {
			return cleanPath(u.Path)
		}
		host, p = u.Hostname(), u.Path
	case isSCP(r):
		hostPart, rest, _ := strings.Cut(r, ":")
		if _, h, ok := strings.Cut(hostPart, "@"); ok {
			hostPart = h
		}
		host, p = hostPart, rest
	default:
		return cleanPath(r)
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	if host == "" {
		return p
	}
	return strings.ToLower(host) + "/" + p
}

// isSCP reports whether r is git's scp-like form, [user@]host:path. A
// Windows drive path such as C:\repo is not.
func isSCP(r string) bool {
	i := strings.Index(r, ":")
	if i <= 0 {
		return false
	}
	if strings.ContainsAny(r[:i], `/\`) {
		return false
	}
	if i == 1 { // a drive letter
		return false
	}
	return true
}

func cleanPath(p string) string {
	return strings.TrimSuffix(strings.TrimRight(filepath.ToSlash(filepath.Clean(p)), "/"), ".git")
}

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
