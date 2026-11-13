package gitlab

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"strconv"
	"strings"
)

var _ forge.TeamClient = (*Client)(nil)

func (c *Client) Login(ctx context.Context) (string, error) {
	var u struct{ Username string }
	_, err := c.http.Do(ctx, "GET", "/user", nil, &u)
	if err == nil && u.Username == "" {
		err = fmt.Errorf("forge returned no authenticated login")
	}
	return u.Username, err
}
func (c *Client) ProtectedBase(ctx context.Context, branch string) (bool, error) {
	// Examine every matching protected rule: a permissive wildcard must not be
	// hidden by a stricter exact rule (GitLab uses the most permissive match).
	var rules []struct {
		Name string
		Push []struct {
			Level   int  `json:"access_level"`
			UserID  *int `json:"user_id"`
			GroupID *int `json:"group_id"`
		} `json:"push_access_levels"`
	}
	for page := 1; ; page++ {
		var list []struct {
			Name string
			Push []struct {
				Level   int  `json:"access_level"`
				UserID  *int `json:"user_id"`
				GroupID *int `json:"group_id"`
			} `json:"push_access_levels"`
		}
		h, err := c.http.Do(ctx, "GET", c.project+"/protected_branches?per_page=100&page="+strconv.Itoa(page), nil, &list)
		if err != nil {
			return false, fmt.Errorf("protection couldn't be confirmed (token needs read_api and access to protected branches): %w", err)
		}
		rules = append(rules, list...)
		if !forge.NextPage(h, page) {
			break
		}
	}
	matched := false
	for _, r := range rules {
		if !wildcard(r.Name, branch) {
			continue
		}
		matched = true
		if len(r.Push) == 0 {
			return false, nil
		}
		for _, p := range r.Push {
			if p.Level != 0 || p.UserID != nil || p.GroupID != nil {
				return false, nil
			}
		}
	}
	if !matched {
		return false, nil
	}
	var project struct {
		Disable bool `json:"disable_overriding_approvers_per_merge_request"`
	}
	_, err := c.http.Do(ctx, "GET", c.project+"/approvals", nil, &project)
	if err != nil {
		return false, fmt.Errorf("approval rules couldn't be confirmed (token needs read_api): %w", err)
	}
	if !project.Disable {
		return false, nil
	}
	for page := 1; ; page++ {
		var list []struct {
			Count    int                     `json:"approvals_required"`
			All      bool                    `json:"applies_to_all_protected_branches"`
			Branches []struct{ Name string } `json:"protected_branches"`
		}
		h, err := c.http.Do(ctx, "GET", c.project+"/approval_rules?per_page=100&page="+strconv.Itoa(page), nil, &list)
		if err != nil {
			return false, fmt.Errorf("approval rules couldn't be confirmed (token needs read_api): %w", err)
		}
		for _, r := range list {
			applies := r.All || len(r.Branches) == 0
			for _, b := range r.Branches {
				applies = applies || wildcard(b.Name, branch)
			}
			if applies && r.Count > 0 {
				return true, nil
			}
		}
		if !forge.NextPage(h, page) {
			return false, nil
		}
	}
}
func wildcard(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
func (c *Client) ListOpenMRs(ctx context.Context) ([]forge.MR, error) {
	var out []forge.MR
	for page := 1; ; page++ {
		var list []mergeRequest
		h, err := c.http.Do(ctx, "GET", c.project+"/merge_requests?state=opened&per_page=100&page="+strconv.Itoa(page), nil, &list)
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
	var p mergeRequest
	_, err := c.http.Do(ctx, "PUT", fmt.Sprintf("%s/merge_requests/%d", c.project, n), map[string]string{"title": r.Title, "description": r.Body}, &p)
	if err != nil {
		return nil, err
	}
	return mr(p), nil
}
func (c *Client) MarkReady(ctx context.Context, n int) error {
	p, err := c.GetMR(ctx, n)
	if err != nil {
		return err
	}
	title := strings.TrimPrefix(strings.TrimPrefix(p.Title, "Draft: "), "WIP: ")
	_, err = c.UpdateMR(ctx, n, forge.OpenRequest{Title: title, Body: p.Body})
	return err
}
