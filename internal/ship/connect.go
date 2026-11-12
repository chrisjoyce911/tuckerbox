package ship

import (
	"context"
	"fmt"
	"net/http"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/github"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/gitlab"
)

func Connect(ctx context.Context, cfg *config.Config, httpClient *http.Client) (forge.Client, func(string) string, error) {
	target := cfg.ForgeTarget
	if target.Kind == "" || target.Host == "" {
		return nil, nil, fmt.Errorf("unknown forge; set [forge] kind")
	}
	token, err := forge.ReadToken(ctx, target, cfg.Forges[target.Host].Credentials())
	if err != nil {
		return nil, nil, err
	}
	if !token.Available() {
		variables := ""
		switch target.Host {
		case "github.com":
			variables = "; GH_TOKEN or GITHUB_TOKEN"
		case "gitlab.com":
			variables = "; GITLAB_TOKEN"
		}
		return nil, nil, fmt.Errorf("no token for %s; set machine [forges.%q] token_env or borrow_cli_token%s", target.Host, target.Host, variables)
	}
	h, err := forge.NewHTTP(target.APIURL, token, httpClient)
	if err != nil {
		return nil, nil, err
	}
	var c forge.Client
	switch target.Kind {
	case forge.GitHub:
		c, err = github.New(target, h)
	case forge.GitLab:
		c, err = gitlab.New(target, h)
	default:
		err = fmt.Errorf("unknown forge kind")
	}
	return c, h.Mask, err
}
