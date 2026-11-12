// Package github implements the GitHub REST and review-thread GraphQL APIs.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type Client struct {
	http                    *forge.HTTP
	repo, owner, name, host string
}

var _ forge.Client = (*Client)(nil)

func New(target forge.Target, h *forge.HTTP) (*Client, error) {
	owner, name, ok := strings.Cut(target.Project, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return nil, fmt.Errorf("GitHub requires owner/repository")
	}
	return &Client{http: h, repo: "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name), owner: owner, name: name, host: target.Host}, nil
}
func (c *Client) Access(ctx context.Context) (forge.Access, error) {
	var r struct {
		DefaultBranch string `json:"default_branch"`
		Permissions   struct{ Push bool }
	}
	_, err := c.http.Do(ctx, "GET", c.repo, nil, &r)
	return forge.Access{Read: err == nil, Push: r.Permissions.Push, DefaultBranch: r.DefaultBranch}, err
}

type pull struct {
	Number             int
	HTMLURL            string `json:"html_url"`
	Title, Body, State string
	Merged             bool
	MergeableState     string `json:"mergeable_state"`
	Head               struct{ SHA string }
	Base               struct{ Ref string }
}

func mr(p pull) *forge.MR {
	state := p.State
	if p.Merged {
		state = "merged"
	}
	return &forge.MR{Number: p.Number, URL: p.HTMLURL, Title: p.Title, Body: p.Body, State: state, Head: p.Head.SHA, Base: p.Base.Ref, MergeStatus: p.MergeableState, Mergeable: p.MergeableState == "clean", Behind: p.MergeableState == "behind"}
}
func (c *Client) FindMR(ctx context.Context, branch string) (*forge.MR, error) {
	for page := 1; ; page++ {
		var list []pull
		h, err := c.http.Do(ctx, "GET", c.repo+"/pulls?state=open&head="+url.QueryEscape(c.owner+":"+branch)+"&per_page=100&page="+strconv.Itoa(page), nil, &list)
		if err != nil {
			return nil, err
		}
		if len(list) > 0 {
			return mr(list[0]), nil
		}
		if !forge.NextPage(h, page) {
			return nil, nil
		}
	}
}
func (c *Client) OpenMR(ctx context.Context, r forge.OpenRequest) (*forge.MR, error) {
	var p pull
	_, err := c.http.Do(ctx, "POST", c.repo+"/pulls", map[string]any{"title": r.Title, "body": r.Body, "head": r.Branch, "base": r.Base, "draft": false}, &p)
	if err != nil {
		return nil, err
	}
	if len(r.Labels) > 0 {
		if _, err := c.http.Do(ctx, "POST", fmt.Sprintf("%s/issues/%d/labels", c.repo, p.Number), map[string]any{"labels": r.Labels}, nil); err != nil {
			return mr(p), err
		}
	}
	return mr(p), nil
}
func (c *Client) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	var p pull
	_, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/pulls/%d", c.repo, n), nil, &p)
	if err != nil {
		return nil, err
	}
	return mr(p), nil
}
func (c *Client) Merge(ctx context.Context, n int, sha, method string) error {
	if sha == "" {
		return fmt.Errorf("merge requires a head SHA")
	}
	if method != "merge" && method != "squash" && method != "rebase" {
		return fmt.Errorf("invalid merge method")
	}
	var result struct {
		Merged  bool
		Message string
	}
	_, err := c.http.Do(ctx, "PUT", fmt.Sprintf("%s/pulls/%d/merge", c.repo, n), map[string]string{"sha": sha, "merge_method": method}, &result)
	if err == nil && !result.Merged {
		return fmt.Errorf("forge refused merge: %s", result.Message)
	}
	return err
}
func checkState(status, conclusion string) string {
	if status != "completed" {
		return "pending"
	}
	switch conclusion {
	case "success", "neutral", "skipped":
		return "green"
	default:
		return "red"
	}
}
func (c *Client) Checks(ctx context.Context, sha, base string) (forge.Checks, error) {
	out := forge.Checks{}
	for page := 1; ; page++ {
		var r struct {
			CheckRuns []struct {
				ID                       int64
				Name, Status, Conclusion string
				HTMLURL                  string `json:"html_url"`
				DetailsURL               string `json:"details_url"`
				Output                   struct{ Title, Summary, Text string }
			} `json:"check_runs"`
		}
		h, err := c.http.Do(ctx, "GET", c.repo+"/commits/"+url.PathEscape(sha)+"/check-runs?filter=latest&per_page=100&page="+strconv.Itoa(page), nil, &r)
		if err != nil {
			return out, err
		}
		for _, j := range r.CheckRuns {
			logID := ""
			u, err := url.Parse(j.DetailsURL)
			if err == nil && u.Hostname() == c.host && strings.HasPrefix(u.Path, "/"+c.owner+"/"+c.name+"/actions/runs/") {
				parts := strings.Split(u.Path, "/")
				for i, p := range parts {
					if p == "job" && i+1 < len(parts) {
						if _, err := strconv.ParseInt(parts[i+1], 10, 64); err == nil {
							logID = parts[i+1]
						}
					}
				}
			}
			out.Jobs = append(out.Jobs, forge.Job{ID: strconv.FormatInt(j.ID, 10), Name: j.Name, URL: j.HTMLURL, State: checkState(j.Status, j.Conclusion), Summary: strings.Join([]string{j.Output.Title, j.Output.Summary, j.Output.Text}, "\n"), LogID: logID})
		}
		if !forge.NextPage(h, page) {
			break
		}
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		var r struct {
			Statuses []struct {
				ID                          int64
				Context, State, Description string
				TargetURL                   string `json:"target_url"`
			}
		}
		h, err := c.http.Do(ctx, "GET", c.repo+"/commits/"+url.PathEscape(sha)+"/status?per_page=100&page="+strconv.Itoa(page), nil, &r)
		if err != nil {
			return out, err
		}
		// GitHub returns statuses newest first; earlier statuses for a context are obsolete.
		for _, s := range r.Statuses {
			if seen[s.Context] {
				continue
			}
			seen[s.Context] = true
			state := "pending"
			switch s.State {
			case "success":
				state = "green"
			case "error", "failure":
				state = "red"
			}
			out.Jobs = append(out.Jobs, forge.Job{ID: strconv.FormatInt(s.ID, 10), Name: s.Context, State: state, URL: s.TargetURL, Summary: s.Description})
		}
		if !forge.NextPage(h, page) {
			break
		}
	}
	var rules []struct {
		Type       string
		Parameters struct {
			RequiredStatusChecks []struct{ Context string } `json:"required_status_checks"`
		}
	}
	_, err := c.http.Do(ctx, "GET", c.repo+"/rules/branches/"+url.PathEscape(base), nil, &rules)
	if err != nil {
		if e, ok := err.(*forge.APIError); !ok || (e.Status != 403 && e.Status != 404) {
			return out, err
		}
	} else {
		for _, r := range rules {
			if r.Type == "required_status_checks" {
				for _, s := range r.Parameters.RequiredStatusChecks {
					out.Required = append(out.Required, s.Context)
				}
			}
		}
	}
	return out, nil
}
func (c *Client) Log(ctx context.Context, j forge.Job) (forge.Log, error) {
	if j.LogID == "" {
		return forge.Log{Text: j.Summary + "\n" + j.URL}, nil
	}
	if _, err := strconv.ParseInt(j.LogID, 10, 64); err != nil {
		return forge.Log{}, fmt.Errorf("invalid job id")
	}
	return c.http.JobLog(ctx, c.repo+"/actions/jobs/"+j.LogID+"/logs")
}
