package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestBoardCLI(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, RequireApproval: true})
	for _, args := range [][]string{{"board", "--json"}, {"board", "--change", "add-login", "--json"}} {
		res := f.tb(args...)
		if res.code != 0 {
			t.Fatalf("%v: %d %s", args, res.code, res.stderr)
		}
		var doc board.Document
		if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Version != board.Version || len(doc.Cards) != 1 || doc.Cards[0].Column != "proposed" {
			t.Fatal(doc)
		}
		if len(args) > 2 && (doc.Detail == nil || doc.Detail.Change != "add-login" || doc.Detail.Tasks[0].Number != "1.1") {
			t.Fatal(doc)
		}
	}
	text := f.tb("board")
	if text.code != 0 || !strings.Contains(text.stdout, "add login") || !strings.Contains(text.stdout, "proposed") {
		t.Fatal(text)
	}
	missing := f.tb("board", "--change", "unknown", "--json")
	if missing.code == 0 || !strings.Contains(missing.stdout, `"code": "unknown_change"`) {
		t.Fatal(missing)
	}
	for _, args := range [][]string{{"board", "extra"}, {"board", "--change"}, {"board", "--wat"}, {"board", "--change", "../bad"}} {
		if res := f.tb(args...); res.code != 2 {
			t.Fatalf("%v: %+v", args, res)
		}
	}
}
func TestBoardCLIErrorDocument(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{NoConfig: true})
	outside := t.TempDir()
	for _, root := range []string{f.repo, outside} {
		var out, stderr bytes.Buffer
		app := App{Stdout: &out, Stderr: &stderr, Getwd: func() (string, error) { return root, nil }}
		if code := app.Run([]string{"board", "--json"}); code != 1 {
			t.Fatalf("exit %d", code)
		}
		var doc struct {
			Version int
			Error   struct{ Code, Message string }
		}
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Version != 1 || doc.Error.Code != "not_a_repository" || doc.Error.Message == "" {
			t.Fatal(out.String())
		}
	}
	// Config errors should be distinguishable from a missing repository.
	testenv.WriteFile(t, filepath.Join(f.repo, ".tuckerbox", "config.toml"), "bad-key = true\n")
	result := f.tb("board", "--json")
	if result.code != 1 || !strings.Contains(result.stdout, `"code": "board_error"`) {
		t.Fatal(result)
	}
}

func TestBoardJSONDoesNotWriteRepoFiles(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	before := testenv.Git(t, f.repo, "status", "--porcelain", "--untracked-files=all")
	if res := f.tb("board", "--json"); res.code != 0 {
		t.Fatal(res)
	}
	after := testenv.Git(t, f.repo, "status", "--porcelain", "--untracked-files=all")
	if before != after {
		t.Fatal("board changed repository", after)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "board.json")); !os.IsNotExist(err) {
		t.Fatal("board created state")
	}
}

func TestBoardCLIKeepsBrokenCard(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"good", "broken"}})
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "broken", "tuckerbox.toml"), "title = [\n")
	res := f.tb("board", "--change", "broken", "--json")
	if res.code != 0 {
		t.Fatalf("%d %s", res.code, res.stderr)
	}
	var doc board.Document
	if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cards) != 2 || doc.Detail == nil {
		t.Fatal(doc)
	}
	for _, c := range doc.Cards {
		if (c.Change == "broken") != (c.Error != "") {
			t.Fatal(c)
		}
	}
	text := f.tb("board")
	if text.code != 0 || !strings.Contains(text.stdout, "ERROR") {
		t.Fatal(text)
	}
}

func TestBoardLayoutsAndCheck(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	wt := filepath.Join(filepath.Dir(f.repo), "repo-worktrees", "add-login")
	testenv.Git(t, f.repo, "worktree", "add", "-qb", "add-login", wt)
	st, err := statedir.Resolve(context.Background(), wt)
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: "add-login", Worktree: wt, State: runs.Blocked, Step: "build"}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	original, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, layout := range []string{"compact", "steps"} {
		testenv.WriteFile(t, configPath, string(original)+"\n[board]\ncolumns = '"+layout+"'\n")
		want := "in_progress"
		if layout == "steps" {
			want = "step:build"
		}
		for _, args := range [][]string{{"board"}, {"board", "--json"}} {
			res := f.tb(args...)
			if res.code != 0 || !strings.Contains(res.stdout, want) {
				t.Fatal(res)
			}
			if len(args) == 2 {
				var doc board.Document
				if err := json.Unmarshal([]byte(res.stdout), &doc); err != nil {
					t.Fatal(err)
				}
				if doc.Cards[0].Column != want || doc.Cards[0].Badge != "build" {
					t.Fatal(doc.Cards)
				}
			} else if !strings.Contains(res.stdout, "BADGE") {
				t.Fatal(res)
			}
		}
	}
	testenv.WriteFile(t, configPath, string(original)+"\n[board]\ncolumns = 'wide'\n")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, ".tuckerbox/config.toml") || !strings.Contains(res.stderr, "board.columns") || !strings.Contains(res.stderr, "wide") {
		t.Fatal(res)
	}
}
