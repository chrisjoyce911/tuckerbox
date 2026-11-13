package github

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"net/url"
	"strconv"
)

var _ forge.TeamClient = (*Client)(nil)

func (c *Client) Login(ctx context.Context) (string, error) {
	var u struct{ Login string }
	_, err := c.http.Do(ctx, "GET", "/user", nil, &u)
	if err == nil && u.Login == "" {
		err = fmt.Errorf("forge returned no authenticated login")
	}
	return u.Login, err
}

// Required reviews, enforced for administrators, with no bypass actors close
// direct pushes as well as unreviewed merges. Fail closed on unreadable rules.
func (c *Client) ProtectedBase(ctx context.Context, branch string) (bool, error) {
	var p struct {
		EnforceAdmins struct{ Enabled bool } `json:"enforce_admins"`
		Reviews       *struct {
			Count  int                                `json:"required_approving_review_count"`
			Bypass struct{ Users, Teams, Apps []any } `json:"bypass_pull_request_allowances"`
		} `json:"required_pull_request_reviews"`
	}
	_, err := c.http.Do(ctx, "GET", c.repo+"/branches/"+url.PathEscape(branch)+"/protection", nil, &p)
	if err != nil {
		return false, fmt.Errorf("protection couldn't be confirmed (token needs repository Administration: read): %w", err)
	}
	return p.Reviews != nil && p.Reviews.Count > 0 && p.EnforceAdmins.Enabled && len(p.Reviews.Bypass.Users)+len(p.Reviews.Bypass.Teams)+len(p.Reviews.Bypass.Apps) == 0, nil
}
func (c *Client) ListOpenMRs(ctx context.Context) ([]forge.MR, error) {
	var out []forge.MR
	for page := 1; ; page++ {
		var list []pull
		h, err := c.http.Do(ctx, "GET", c.repo+"/pulls?state=open&per_page=100&page="+strconv.Itoa(page), nil, &list)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			out = append(out, *mr(p))
		}
		if !forge.NextPage(h, page) {
			return out, nil
		}
	}
}
func (c *Client) UpdateMR(ctx context.Context, n int, r forge.OpenRequest) (*forge.MR, error) {
	var p pull
	_, err := c.http.Do(ctx, "PATCH", fmt.Sprintf("%s/pulls/%d", c.repo, n), map[string]any{"title": r.Title, "body": r.Body}, &p)
	if err != nil {
		return nil, err
	}
	return mr(p), nil
}
func (c *Client) MarkReady(ctx context.Context, n int) error {
	var p struct {
		NodeID string `json:"node_id"`
		Draft  bool
	}
	_, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/pulls/%d", c.repo, n), nil, &p)
	if err != nil {
		return err
	}
	if !p.Draft {
		return nil
	}
	if p.NodeID == "" {
		return fmt.Errorf("forge returned no pull request node ID")
	}
	var result struct {
		Data struct {
			Ready struct{ PullRequest struct{ IsDraft bool } } `json:"markPullRequestReadyForReview"`
		}
		Errors []struct{ Message string }
	}
	_, err = c.http.GraphQL(ctx, map[string]any{"query": `mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft}}}`, "variables": map[string]any{"id": p.NodeID}}, &result)
	if err == nil && len(result.Errors) > 0 {
		err = fmt.Errorf("GitHub GraphQL: %s", result.Errors[0].Message)
	}
	if err == nil && result.Data.Ready.PullRequest.IsDraft {
		err = fmt.Errorf("pull request remains draft")
	}
	return err
}
