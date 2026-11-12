package configsave

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// Unimplemented methods deliberately panic through the embedded interface:
// config saves must call Access/OpenMR only, never Merge or a run's stages.
type configForge struct {
	forge.Client
	root    string
	t       *testing.T
	opened  int
	request forge.OpenRequest
}

func (f *configForge) Access(context.Context) (forge.Access, error) {
	return forge.Access{Read: true, Push: true, DefaultBranch: "main"}, nil
}
func (f *configForge) OpenMR(_ context.Context, r forge.OpenRequest) (*forge.MR, error) {
	f.opened++
	f.request = r
	return &forge.MR{Number: 1, URL: "https://github.com/test/shop/pull/1"}, nil
}
func prFixture(t *testing.T, merge bool) (Request, *configForge, string) {
	t.Helper()
	for k, v := range testenv.NoNetwork {
		t.Setenv(k, v)
	}
	root := t.TempDir()
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, root, "init", "-q", "-b", "main")
	testenv.Git(t, root, "config", "user.name", "Test")
	testenv.Git(t, root, "config", "user.email", "test@example.invalid")
	testenv.Git(t, root, "config", "commit.gpgsign", "false")
	testenv.Git(t, root, "config", "core.autocrlf", "false")
	data := config.Starter
	if merge {
		data = strings.Replace(data, `ship = "local"`, `ship = "merge"`, 1)
	}
	path := filepath.Join(root, config.RepoFile)
	testenv.WriteFile(t, path, data)
	testenv.Git(t, root, "add", ".")
	testenv.Git(t, root, "commit", "-qm", "base")
	testenv.Git(t, root, "init", "--bare", bare)
	testenv.Git(t, root, "remote", "add", "origin", bare)
	testenv.Git(t, root, "push", "-u", "origin", "main")
	req := Request{Options: config.Options{RepoRoot: root, MachinePath: filepath.Join(t.TempDir(), "config.toml"), Remote: "https://github.com/test/shop.git"}, StateDir: t.TempDir(), Path: path, IfMatch: Revision([]byte(data)), Edits: []Edit{{Path: []string{"max_parallel"}, Value: int64(7)}}}
	return req, &configForge{root: root, t: t}, bare
}
func TestPullRequestCheckoutUntouchedAndNeverMerge(t *testing.T) {
	for _, merge := range []bool{false, true} {
		name := "local ship mode"
		if merge {
			name = "merge ship mode"
		}
		t.Run(name, func(t *testing.T) {
			req, f, bare := prFixture(t, merge)
			before, _ := Read(req.Path)
			dirty := append(append([]byte{}, before...), []byte("\n# checkout edit kept\n")...)
			if err := os.WriteFile(req.Path, dirty, 0644); err != nil {
				t.Fatal(err)
			}
			res := NewResult(false, false)
			res.Target = "pr"
			err := PullRequest(context.Background(), req, "origin", f, nil, &res)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := Read(req.Path)
			if string(after) != string(dirty) {
				t.Fatal("checkout modified")
			}
			if f.opened != 1 || res.Link == "" || f.request.Base != "main" || !strings.HasPrefix(f.request.Branch, "tuckerbox/config-") {
				t.Fatalf("bad request %+v result %+v", f.request, res)
			}
			if !strings.HasPrefix(f.request.Title, "Configure Tuckerbox: max_parallel") {
				t.Fatal(f.request.Title)
			}
			branchFile := testenv.Git(t, bare, "show", f.request.Branch+":"+filepath.ToSlash(config.RepoFile))
			if !strings.Contains(branchFile, "max_parallel = 7") || strings.Contains(branchFile, "checkout edit") {
				t.Fatalf("wrong branch file %s", branchFile)
			}
			if got := testenv.Git(t, req.Options.RepoRoot, "diff", "--name-only", "main..."+f.request.Branch); got != filepath.ToSlash(config.RepoFile) {
				t.Fatalf("extra committed files %s", got)
			}
			if got := testenv.Git(t, req.Options.RepoRoot, "rev-list", "--count", "main.."+f.request.Branch); got != "1" {
				t.Fatalf("commits %s", got)
			}
			if got := testenv.Git(t, req.Options.RepoRoot, "worktree", "list", "--porcelain"); strings.Contains(got, "tuckerbox-config-") {
				t.Fatalf("temp worktree retained: %s", got)
			}
		})
	}
}
func TestPullRequestBaseMoved(t *testing.T) {
	req, f, bare := prFixture(t, false)
	before, _ := Read(req.Path)
	newData := strings.Replace(string(before), "max_parallel = 1", "max_parallel = 3", 1)
	testenv.WriteFile(t, req.Path, newData)
	testenv.Git(t, req.Options.RepoRoot, "add", ".")
	testenv.Git(t, req.Options.RepoRoot, "commit", "-qm", "another save")
	testenv.Git(t, req.Options.RepoRoot, "push", "origin", "main")
	res := NewResult(false, false)
	err := PullRequest(context.Background(), req, "origin", f, nil, &res)
	var se *SaveError
	if !errors.As(err, &se) || se.Code != "changed_since_read" {
		t.Fatalf("got %v", err)
	}
	if f.opened != 0 {
		t.Fatal("opened stale save")
	}
	if refs := testenv.Git(t, bare, "for-each-ref", "--format=%(refname)", "refs/heads/tuckerbox"); refs != "" {
		t.Fatalf("pushed stale save: %s", refs)
	}
}
func TestPullRequestDryRun(t *testing.T) {
	req, f, bare := prFixture(t, false)
	req.DryRun = true
	res := NewResult(false, true)
	if err := PullRequest(context.Background(), req, "origin", f, nil, &res); err != nil {
		t.Fatal(err)
	}
	if f.opened != 0 {
		t.Fatal("dry run opened MR")
	}
	if refs := testenv.Git(t, bare, "for-each-ref", "--format=%(refname)", "refs/heads/tuckerbox"); refs != "" {
		t.Fatalf("dry run pushed: %s", refs)
	}
	if refs := testenv.Git(t, req.Options.RepoRoot, "for-each-ref", "--format=%(refname)", "refs/heads/tuckerbox"); refs != "" {
		t.Fatalf("dry run created branch: %s", refs)
	}
}
