package gitremote

import (
	"net/url"
	"path/filepath"
	"strings"
)

// Normalize turns a git remote URL into a plain host/path form, so the
// same repository matches however it was cloned. For example
// "git@github.com:owner/repo.git", "https://github.com/owner/repo" and
// "ssh://git@github.com/owner/repo.git" all become "github.com/owner/repo".
// The host is lower-cased; the user, port, ".git" suffix and trailing
// slashes are dropped. A local path is returned cleaned, with forward slashes.
func Normalize(remote string) string {
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
	case IsSCP(r):
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
func IsSCP(r string) bool {
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
