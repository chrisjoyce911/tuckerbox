package runs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/github"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/gitlab"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

type httpForge struct {
	mu                 sync.Mutex
	t                  *testing.T
	kind               forge.Kind
	server             *httptest.Server
	worktree, scenario string
	open, merged       bool
	body, title        string
	posts, merges      int
	reply              string
	checked            []string
}

func (h *httpForge) write(w http.ResponseWriter, v any) {
	h.t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.t.Error(err)
	}
}
func (h *httpForge) head() string {
	tip := testenv.Git(h.t, h.worktree, "ls-remote", "origin", "refs/heads/add-login")
	fields := strings.Fields(tip)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
func (h *httpForge) fixed() bool {
	_, err := os.Stat(filepath.Join(h.worktree, "fixed.go"))
	return err == nil
}
func (h *httpForge) mr() map[string]any {
	state := "open"
	if h.merged {
		state = "closed"
	}
	sha := h.head()
	if h.kind == forge.GitLab {
		state = "opened"
		if h.merged {
			state = "merged"
		}
		return map[string]any{"iid": 1, "web_url": h.server.URL + "/mr/1", "state": state, "sha": sha, "title": h.title, "description": h.body, "target_branch": "main", "detailed_merge_status": "mergeable"}
	}
	return map[string]any{"number": 1, "html_url": h.server.URL + "/mr/1", "state": state, "merged": h.merged, "title": h.title, "body": h.body, "head": map[string]any{"sha": sha}, "base": map[string]any{"ref": "main"}, "mergeable_state": "clean"}
}
func (h *httpForge) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer only-test-token" {
		h.t.Error("fake API did not receive its test token")
		w.WriteHeader(401)
		return
	}
	path := r.URL.Path
	if h.kind == forge.GitLab {
		path = strings.TrimPrefix(path, "/api/v4/projects/sam/app")
	} else {
		path = strings.TrimPrefix(path, "/repos/sam/app")
	}
	var input map[string]any
	if r.Method != "GET" {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			h.t.Error(err)
		}
	}
	collection := "/pulls"
	if h.kind == forge.GitLab {
		collection = "/merge_requests"
	}
	switch {
	case path == "" && r.Method == "GET":
		if h.kind == forge.GitLab {
			h.write(w, map[string]any{"default_branch": "main", "permissions": map[string]any{"project_access": map[string]any{"access_level": 40}}})
		} else {
			h.write(w, map[string]any{"default_branch": "main", "permissions": map[string]any{"push": true}})
		}
	case path == collection && r.Method == "GET":
		result := []any{}
		if h.open && !h.merged {
			result = append(result, h.mr())
		}
		h.write(w, result)
	case path == collection && r.Method == "POST":
		h.posts++
		h.open = true
		h.title, _ = input["title"].(string)
		key := "body"
		if h.kind == forge.GitLab {
			key = "description"
		}
		h.body, _ = input[key].(string)
		h.write(w, h.mr())
	case path == collection+"/1" && r.Method == "GET":
		h.write(w, h.mr())
	case path == collection+"/1/merge" && r.Method == "PUT":
		sha, _ := input["sha"].(string)
		green := false
		for _, checked := range h.checked {
			if checked == sha {
				green = true
			}
		}
		if sha != h.head() || !green || (h.scenario == "ci" && !h.fixed()) {
			h.t.Error("merge did not pin a green head")
			w.WriteHeader(409)
			return
		}
		h.merged = true
		h.merges++
		if h.kind == forge.GitLab {
			h.write(w, h.mr())
		} else {
			h.write(w, map[string]any{"merged": true})
		}
	case strings.Contains(path, "/check-runs"):
		parts := strings.Split(path, "/")
		h.checked = append(h.checked, parts[2])
		conclusion := "success"
		if h.scenario == "ci" && !h.fixed() {
			conclusion = "failure"
		}
		h.write(w, map[string]any{"check_runs": []any{map[string]any{"id": 101, "name": "go test", "status": "completed", "conclusion": conclusion, "html_url": h.server.URL + "/job/101", "details_url": h.server.URL + "/sam/app/actions/runs/1/job/101"}}})
	case strings.HasSuffix(path, "/status"):
		h.write(w, map[string]any{"statuses": []any{}})
	case strings.HasPrefix(path, "/rules/branches/"):
		h.write(w, []any{})
	case path == "/pipelines":
		sha := r.URL.Query().Get("sha")
		h.checked = append(h.checked, sha)
		status := "success"
		if h.scenario == "ci" && !h.fixed() {
			status = "failed"
		}
		h.write(w, []any{map[string]any{"id": 1, "sha": sha, "status": status}})
	case path == "/pipelines/1/jobs":
		status := "success"
		if h.scenario == "ci" && !h.fixed() {
			status = "failed"
		}
		h.write(w, []any{map[string]any{"id": 101, "name": "go test", "status": status, "web_url": h.server.URL + "/job/101"}})
	case path == "/actions/jobs/101/logs" || path == "/jobs/101/trace":
		fmt.Fprint(w, "\x1b[31monly-test-token\x1b[0m\njob failed\n")
	case path == "/merge_requests/1/discussions" && r.Method == "GET":
		discussions := []any{}
		if h.scenario == "review" {
			notes := []any{map[string]any{"id": 1, "body": "please fix validation", "resolvable": true, "resolved": false, "author": map[string]any{"id": 5, "username": "maintainer"}, "position": map[string]any{"new_path": "login.go", "new_line": 12}}}
			if h.reply != "" {
				notes = append(notes, map[string]any{"id": 2, "body": h.reply, "author": map[string]any{"id": 5, "username": "maintainer"}})
			}
			discussions = append(discussions, map[string]any{"id": "thread1", "notes": notes})
		}
		h.write(w, discussions)
	case path == "/members/all/5":
		h.write(w, map[string]any{"access_level": 40})
	case path == "/merge_requests/1/discussions/thread1/notes":
		h.reply, _ = input["body"].(string)
		h.write(w, map[string]any{"id": 2})
	case path == "/graphql":
		query, _ := input["query"].(string)
		variables, _ := input["variables"].(map[string]any)
		if strings.Contains(query, "addPullRequestReviewThreadReply") {
			h.reply, _ = variables["body"].(string)
			h.write(w, map[string]any{"data": map[string]any{"addPullRequestReviewThreadReply": map[string]any{"comment": map[string]any{"id": "reply1"}}}})
			return
		}
		nodes := []any{}
		if h.scenario == "review" {
			comments := []any{map[string]any{"id": "c1", "body": "please fix validation", "url": h.server.URL + "/thread/1", "author": map[string]any{"login": "maintainer"}, "authorAssociation": "OWNER"}}
			if h.reply != "" {
				comments = append(comments, map[string]any{"id": "reply1", "body": h.reply, "author": map[string]any{"login": "bot"}, "authorAssociation": "MEMBER"})
			}
			nodes = append(nodes, map[string]any{"id": "thread1", "path": "login.go", "line": 12, "isResolved": false, "comments": map[string]any{"nodes": comments, "pageInfo": map[string]any{"hasNextPage": false}}})
		}
		h.write(w, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{"reviewThreads": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false}}}}}})
	default:
		h.t.Errorf("unexpected fake API request: %s %s", r.Method, r.URL)
		w.WriteHeader(500)
	}
}
func httpShippingSetup(t *testing.T, kind forge.Kind, scenario string) (*fixture, *Engine, *httpForge) {
	t.Helper()
	f, e, _ := shippingSetup(t, config.ShipMR, true, "add-login")
	h := &httpForge{t: t, kind: kind, worktree: f.worktree("add-login"), scenario: scenario}
	h.server = httptest.NewTLSServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.server.Close)
	parsed, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	apiURL := h.server.URL
	if kind == forge.GitLab {
		apiURL += "/api/v4"
	}
	target := forge.Target{Kind: kind, Host: parsed.Hostname(), Project: "sam/app", APIURL: apiURL}
	f.cfg.ForgeTarget = target
	fix := f.cfg.Steps[0]
	fix.ID = "fix_ci"
	fix.Role = "fixer"
	fix.OnReject = ""
	f.cfg.FixSteps = map[string]config.Step{"fix_ci": fix}
	f.cfg.Shipping.FixCI = "fix_ci"
	f.cfg.Shipping.FixReview = "fix_ci"
	f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go", Tick: []string{"1.1"}}, "fix_ci": {Touch: "fixed.go", Summary: "validation fixed"}})
	e.Forge = func(ctx context.Context) (forge.Client, func(string) string, error) {
		token, err := forge.LookupToken(ctx, target, forge.Credentials{TokenEnv: "TEST_TOKEN"}, func(string) string { return "only-test-token" }, nil)
		if err != nil {
			return nil, nil, err
		}
		transport, err := forge.NewHTTP(target.APIURL, token, h.server.Client())
		if err != nil {
			return nil, nil, err
		}
		var client forge.Client
		if kind == forge.GitHub {
			client, err = github.New(target, transport)
		} else {
			client, err = gitlab.New(target, transport)
		}
		return client, transport.Mask, err
	}
	return f, e, h
}
func TestHTTPForgeShippingAcceptance(t *testing.T) {
	for _, kind := range []forge.Kind{forge.GitHub, forge.GitLab} {
		for _, scenario := range []string{"green", "ci", "review", "merge"} {
			t.Run(string(kind)+"/"+scenario, func(t *testing.T) {
				f, e, h := httpShippingSetup(t, kind, scenario)
				want := Ready
				if scenario == "merge" {
					f.cfg.Ship = config.ShipMerge
					want = Merged
				}
				r, err := e.Run(context.Background(), Request{Change: "add-login"})
				h.mu.Lock()
				posts, merges, reply := h.posts, h.merges, h.reply
				h.mu.Unlock()
				if err != nil || r.State != want || posts != 1 {
					t.Fatal(r, err, posts)
				}
				if scenario == "ci" || scenario == "review" {
					if r.ShipFixes != 1 {
						t.Fatal(r.ShipFixes)
					}
				}
				if scenario == "review" && (!strings.Contains(reply, r.LastPush) || !strings.Contains(reply, "validation fixed")) {
					t.Fatal(reply)
				}
				if scenario == "merge" && merges != 1 {
					t.Fatal(merges)
				}
				if scenario == "ci" {
					paths, err := filepath.Glob(filepath.Join(string(f.state("add-login")), "ship", "*", "*.log"))
					if err != nil || len(paths) != 1 {
						t.Fatal(paths, err)
					}
					data, err := os.ReadFile(paths[0])
					if err != nil || strings.Contains(string(data), "only-test-token") || strings.Contains(string(data), "\x1b") {
						t.Fatal(string(data), err)
					}
				}
			})
		}
	}
}

const finalScopedTasks = "## 1. First\n- [ ] 1.1 first part\n## 2. Finish\n- [ ] 2.1 final part\n## 3. Shipping\n- [ ] 3.1 (ship) archive and open request\n```md\n- [ ] 3.2 (ship) example only\n```\n"

func TestHTTPPartialAndFinalScopes(t *testing.T) {
	for _, kind := range []forge.Kind{forge.GitHub, forge.GitLab} {
		t.Run(string(kind), func(t *testing.T) {
			f, e, h := httpShippingSetup(t, kind, "green")
			f.withTasks(finalScopedTasks, "add-login")
			testenv.Git(t, f.dir, "push", "origin", "main")
			f.cfg.Ship = config.ShipMerge
			f.env.SetPlan(t, map[string]testenv.Action{"build": {Touch: "login.go", Tick: []string{"1.1"}}})
			r, err := e.Run(context.Background(), Request{Change: "add-login", Scope: "1"})
			if err != nil || r.State != Ready || r.Complete || r.ShippingStarted {
				t.Fatal(r, err)
			}
			if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); tip != "" {
				t.Fatal(tip)
			}
			if archives, _ := filepath.Glob(filepath.Join(r.Worktree, "openspec", "changes", "archive", "*-add-login")); len(archives) != 0 {
				t.Fatal(archives)
			}
			h.mu.Lock()
			posts, merges := h.posts, h.merges
			h.mu.Unlock()
			if posts != 0 || merges != 0 {
				t.Fatal(posts, merges)
			}
			// The last group in mr mode archives the completed change and its ship task.
			f.cfg.Ship = config.ShipMR
			f.env.SetPlan(t, map[string]testenv.Action{"build": {Tick: []string{"2.1"}}})
			r, err = e.Run(context.Background(), Request{Change: "add-login", Scope: "2"})
			if err != nil || r.State != Ready || !r.Complete || r.MR == nil || r.Scope.Spec != "2" {
				t.Fatal(r, err)
			}
			data, err := os.ReadFile(filepath.Join(r.ArchivePath, "tasks.md"))
			if err != nil || !strings.Contains(string(data), "- [x] 3.1 (ship)") || !strings.Contains(string(data), "- [ ] 3.2 (ship) example only") {
				t.Fatal(string(data), err)
			}
			if commit := testenv.Git(t, r.Worktree, "log", "--format=%s", "-1"); commit != "Archive add-login" {
				t.Fatal(commit)
			}
		})
	}
}
func TestHTTPWholeChangeWithUntickedTaskDoesNotShip(t *testing.T) {
	f, e, h := httpShippingSetup(t, forge.GitHub, "green")
	f.withTasks(finalScopedTasks, "add-login")
	testenv.Git(t, f.dir, "push", "origin", "main")
	f.cfg.Ship = config.ShipMerge
	r, err := e.Run(context.Background(), Request{Change: "add-login"})
	if err != nil || r.State != Blocked || r.Reason != "tasks not ticked: 2.1" || r.ShippingStarted {
		t.Fatal(r, err)
	}
	h.mu.Lock()
	posts, merges := h.posts, h.merges
	h.mu.Unlock()
	if posts != 0 || merges != 0 {
		t.Fatal(posts, merges)
	}
	if tip := testenv.Git(t, f.dir, "ls-remote", "origin", "refs/heads/add-login"); tip != "" {
		t.Fatal(tip)
	}
}
