// Package forge defines the shared contract for GitHub and GitLab shipping.
package forge

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
)

type Kind string

const (
	GitHub Kind = "github"
	GitLab Kind = "gitlab"
)

type Settings struct {
	Kind   Kind
	APIURL string
}
type Target struct {
	Kind                  Kind
	Host, Project, APIURL string
}

// PushURL asks git for the effective push URL, including pushInsteadOf rules.
func PushURL(ctx context.Context, root, remote string) (string, error) {
	if remote == "" {
		remote = "origin"
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "remote", "get-url", "--push", "--", remote).Output()
	if err != nil {
		return "", fmt.Errorf("reading shipping remote %q: %w", remote, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func Detect(ctx context.Context, remote string, repo Settings, machineKind Kind) (Target, error) {
	return DetectKey(gitremote.Key(ctx, remote), repo, machineKind)
}

// DetectKey accepts the shared normalized remote identity, with SSH aliases resolved.
func DetectKey(key string, repo Settings, machineKind Kind) (Target, error) {
	host, project, ok := strings.Cut(key, "/")
	if !ok || host == "" || project == "" || strings.ContainsAny(host, "@: \\?#") || strings.ContainsAny(project, "\\?#") {
		return Target{}, fmt.Errorf("shipping requires a forge host and project")
	}
	for _, p := range strings.Split(project, "/") {
		if p == "" || p == "." || p == ".." {
			return Target{}, fmt.Errorf("invalid forge project path")
		}
	}
	t := Target{Host: strings.ToLower(host), Project: project}
	switch t.Host {
	case "github.com":
		t.Kind = GitHub
		t.APIURL = "https://api.github.com"
	case "gitlab.com":
		t.Kind = GitLab
		t.APIURL = "https://gitlab.com/api/v4"
	default:
		t.Kind = repo.Kind
		if t.Kind == "" {
			t.Kind = machineKind
		}
		switch t.Kind {
		case GitHub:
			t.APIURL = "https://" + t.Host + "/api/v3"
		case GitLab:
			t.APIURL = "https://" + t.Host + "/api/v4"
		default:
			return Target{}, fmt.Errorf("unknown forge for %s; set [forge] kind", t.Host)
		}
	}
	if repo.Kind != "" && repo.Kind != t.Kind {
		return Target{}, fmt.Errorf("[forge] kind does not match remote host")
	}
	if repo.APIURL != "" {
		u, err := url.Parse(repo.APIURL)
		if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), t.Host) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
			return Target{}, fmt.Errorf("[forge] api_url must be HTTPS on the remote's host, without credentials, query or fragment")
		}
		t.APIURL = strings.TrimRight(u.String(), "/")
	}
	return t, nil
}
