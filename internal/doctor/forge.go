package doctor

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/github"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/gitlab"
)

// CheckForge reports API access. It never tests push by writing a remote ref.
// A custom HTTP client is used only to replay local TLS fixtures in tests.
func CheckForge(ctx context.Context, target forge.Target, credentials forge.Credentials, httpClient *http.Client) string {
	prefix := fmt.Sprintf("forge: %s %s/%s", target.Kind, target.Host, target.Project)
	token, err := forge.ReadToken(ctx, target, credentials)
	help := fmt.Sprintf("machine [forges.%q] token_env or borrow_cli_token; or store one with tuckerbox config set --machine forges.%q.token", target.Host, target.Host)
	switch target.Host {
	case "github.com":
		help += "; GH_TOKEN or GITHUB_TOKEN"
	case "gitlab.com":
		help += "; GITLAB_TOKEN"
	}
	if err != nil {
		return prefix + ": " + err.Error() + " (" + help + ")"
	}
	if !token.Available() {
		cli := "gh"
		if target.Kind == forge.GitLab {
			cli = "glab"
		}
		note := ""
		if _, err := exec.LookPath(cli); err == nil {
			note = fmt.Sprintf("; %s is installed and may hold a token; allow reading it with borrow_cli_token = true", cli)
		}
		return prefix + ": no token; set " + help + note
	}
	h, err := forge.NewHTTP(target.APIURL, token, httpClient)
	if err != nil {
		return prefix + ": " + err.Error()
	}
	var client forge.Client
	switch target.Kind {
	case forge.GitHub:
		client, err = github.New(target, h)
	case forge.GitLab:
		client, err = gitlab.New(target, h)
	default:
		return prefix + ": unknown forge"
	}
	if err != nil {
		return prefix + ": " + err.Error()
	}
	access, err := client.Access(ctx)
	if err != nil {
		if e, ok := err.(*forge.APIError); ok && e.Status == 401 {
			return fmt.Sprintf("%s: %s token was rejected for %s; set %s", prefix, token.Source, target.Host, help)
		}
		return fmt.Sprintf("%s; token source %s; access check failed: %s", prefix, token.Source, h.Mask(err.Error()))
	}
	return fmt.Sprintf("%s; token source %s; read access %t; push access %t", prefix, token.Source, access.Read, access.Push)
}
