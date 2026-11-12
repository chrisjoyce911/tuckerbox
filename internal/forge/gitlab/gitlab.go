// Package gitlab implements GitLab's v4 REST API, including subgroup projects.
package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type Client struct {
	http    *forge.HTTP
	project string
}

var _ forge.Client = (*Client)(nil)

func New(target forge.Target, h *forge.HTTP) (*Client, error) {
	if target.Project == "" {
		return nil, fmt.Errorf("missing GitLab project")
	}
	return &Client{h, "/projects/" + url.PathEscape(target.Project)}, nil
}
func (c *Client) Access(ctx context.Context) (forge.Access, error) {
	var p struct {
		DefaultBranch string `json:"default_branch"`
		Permissions   struct {
			ProjectAccess struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			GroupAccess struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		}
	}
	_, err := c.http.Do(ctx, "GET", c.project, nil, &p)
	return forge.Access{Read: err == nil, Push: p.Permissions.ProjectAccess.AccessLevel >= 30 || p.Permissions.GroupAccess.AccessLevel >= 30, DefaultBranch: p.DefaultBranch}, err
}

type mergeRequest struct {
	IID                       int
	WebURL                    string `json:"web_url"`
	Title, Description, State string
	SHA                       string
	TargetBranch              string `json:"target_branch"`
	DetailedMergeStatus       string `json:"detailed_merge_status"`
}

func mr(p mergeRequest) *forge.MR {
	state := p.State
	if state == "opened" {
		state = "open"
	}
	return &forge.MR{Number: p.IID, URL: p.WebURL, Title: p.Title, Body: p.Description, State: state, Head: p.SHA, Base: p.TargetBranch, MergeStatus: p.DetailedMergeStatus, Mergeable: p.DetailedMergeStatus == "mergeable", Behind: p.DetailedMergeStatus == "need_rebase"}
}
func (c *Client) FindMR(ctx context.Context, branch string) (*forge.MR, error) {
	var r []mergeRequest
	_, err := c.http.Do(ctx, "GET", c.project+"/merge_requests?state=opened&source_branch="+url.QueryEscape(branch)+"&per_page=100", nil, &r)
	if err != nil {
		return nil, err
	}
	if len(r) == 0 {
		return nil, nil
	}
	return mr(r[0]), nil
}
func (c *Client) OpenMR(ctx context.Context, r forge.OpenRequest) (*forge.MR, error) {
	var p mergeRequest
	_, err := c.http.Do(ctx, "POST", c.project+"/merge_requests", map[string]any{"title": r.Title, "description": r.Body, "source_branch": r.Branch, "target_branch": r.Base, "labels": strings.Join(r.Labels, ","), "remove_source_branch": false}, &p)
	if err != nil {
		return nil, err
	}
	return mr(p), nil
}
func (c *Client) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	var p mergeRequest
	_, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/merge_requests/%d", c.project, n), nil, &p)
	if err != nil {
		return nil, err
	}
	return mr(p), nil
}
func (c *Client) Merge(ctx context.Context, n int, sha, method string) error {
	if sha == "" {
		return fmt.Errorf("merge requires a head SHA")
	}
	// GitLab's merge API cannot choose a per-MR merge/rebase method. A rebase
	// request would mutate the branch asynchronously and invalidate the CI pin.
	if method == "rebase" {
		return fmt.Errorf("GitLab cannot select rebase in a pinned merge; configure the project's merge method")
	}
	if method != "merge" && method != "squash" {
		return fmt.Errorf("invalid merge method")
	}
	var p mergeRequest
	_, err := c.http.Do(ctx, "PUT", fmt.Sprintf("%s/merge_requests/%d/merge", c.project, n), map[string]any{"sha": sha, "squash": method == "squash", "should_remove_source_branch": false}, &p)
	if err == nil && p.State != "merged" {
		return fmt.Errorf("GitLab did not confirm merge")
	}
	return err
}
func state(s string) string {
	switch s {
	case "success", "skipped":
		return "green"
	case "failed", "canceled":
		return "red"
	default:
		return "pending"
	}
}
func (c *Client) Checks(ctx context.Context, sha, _ string) (forge.Checks, error) {
	out := forge.Checks{}
	var pipelines []struct {
		ID          int64
		SHA, Status string
		WebURL      string `json:"web_url"`
	}
	_, err := c.http.Do(ctx, "GET", c.project+"/pipelines?sha="+url.QueryEscape(sha)+"&order_by=id&sort=desc&per_page=1", nil, &pipelines)
	if err != nil {
		return out, err
	}
	if len(pipelines) == 0 {
		return out, nil
	}
	p := pipelines[0]
	if p.SHA != sha {
		return out, fmt.Errorf("GitLab pipeline does not match requested head")
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		var jobs []struct {
			ID           int64
			Name, Status string
			AllowFailure bool   `json:"allow_failure"`
			WebURL       string `json:"web_url"`
		}
		h, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/pipelines/%d/jobs?include_retried=false&per_page=100&page=%d", c.project, p.ID, page), nil, &jobs)
		if err != nil {
			return out, err
		}
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID > jobs[j].ID })
		for _, j := range jobs {
			if seen[j.Name] {
				continue
			}
			seen[j.Name] = true
			if j.AllowFailure {
				continue
			}
			id := strconv.FormatInt(j.ID, 10)
			out.Jobs = append(out.Jobs, forge.Job{ID: id, LogID: id, Name: j.Name, State: state(j.Status), URL: j.WebURL})
		}
		if !forge.NextPage(h, page) {
			break
		}
	}
	// Pending pipeline creation/bridges must not look green even when current
	// jobs have finished. Failed pipelines with no failed jobs still count.
	if s := state(p.Status); s != "green" {
		has := false
		for _, j := range out.Jobs {
			if j.State == s {
				has = true
			}
		}
		if !has {
			out.Jobs = append(out.Jobs, forge.Job{ID: "pipeline-" + strconv.FormatInt(p.ID, 10), Name: "pipeline", State: s, URL: p.WebURL, Summary: "Pipeline " + p.Status})
		}
	}
	// A pipeline consisting entirely of allowed failures/skips is reported green.
	if len(out.Jobs) == 0 {
		out.Jobs = append(out.Jobs, forge.Job{ID: "pipeline-" + strconv.FormatInt(p.ID, 10), Name: "pipeline", State: state(p.Status), URL: p.WebURL})
	}
	return out, nil
}
func (c *Client) Log(ctx context.Context, j forge.Job) (forge.Log, error) {
	if j.LogID == "" {
		return forge.Log{Text: j.Summary + "\n" + j.URL}, nil
	}
	if _, err := strconv.ParseInt(j.LogID, 10, 64); err != nil {
		return forge.Log{}, fmt.Errorf("invalid GitLab job id")
	}
	var log forge.Log
	_, err := c.http.Do(ctx, "GET", c.project+"/jobs/"+j.LogID+"/trace", nil, &log)
	return log, err
}
func (c *Client) Threads(ctx context.Context, n int) ([]forge.Thread, error) {
	var out []forge.Thread
	for page := 1; ; page++ {
		var discussions []struct {
			ID    string
			Notes []struct {
				ID                           int64
				Body                         string
				System, Resolvable, Resolved bool
				Author                       struct {
					ID       int
					Username string
				}
				Position struct {
					NewPath string `json:"new_path"`
					OldPath string `json:"old_path"`
					NewLine int    `json:"new_line"`
					OldLine int    `json:"old_line"`
				}
			}
		}
		h, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/merge_requests/%d/discussions?per_page=100&page=%d", c.project, n, page), nil, &discussions)
		if err != nil {
			return nil, err
		}
		for _, d := range discussions {
			t := forge.Thread{ID: d.ID, Resolved: true}
			relevant := false
			for _, note := range d.Notes {
				if note.System {
					continue
				}
				if note.Resolvable {
					relevant = true
					if !note.Resolved {
						t.Resolved = false
					}
					if t.File == "" {
						t.File = note.Position.NewPath
						t.Line = note.Position.NewLine
						if t.File == "" {
							t.File = note.Position.OldPath
							t.Line = note.Position.OldLine
						}
					}
				}
				var member struct {
					AccessLevel int `json:"access_level"`
				}
				_, err := c.http.Do(ctx, "GET", fmt.Sprintf("%s/members/all/%d", c.project, note.Author.ID), nil, &member)
				if err != nil {
					if e, ok := err.(*forge.APIError); !ok || e.Status != 404 {
						return nil, err
					}
				}
				t.Comments = append(t.Comments, forge.Comment{ID: strconv.FormatInt(note.ID, 10), Text: note.Body, Author: note.Author.Username, URL: fmt.Sprintf("%s#note_%d", c.webMR(n), note.ID), Writer: err == nil && member.AccessLevel >= 30})
			}
			if relevant {
				out = append(out, t)
			}
		}
		if !forge.NextPage(h, page) {
			break
		}
	}
	return out, nil
}

// GitLab notes do not include a web URL; callers use the MR URL and note anchor.
func (c *Client) webMR(n int) string {
	return c.http.WebProject(strings.TrimPrefix(c.project, "/projects/")) + "/-/merge_requests/" + strconv.Itoa(n)
}
func (c *Client) Reply(ctx context.Context, n int, thread, text string) error {
	_, err := c.http.Do(ctx, "POST", fmt.Sprintf("%s/merge_requests/%d/discussions/%s/notes", c.project, n, url.PathEscape(thread)), map[string]string{"body": text}, nil)
	return err
}
