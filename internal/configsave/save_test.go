package configsave

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

func saveFixture(t *testing.T) (Request, []byte) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, config.RepoFile)
	data := []byte(config.Starter)
	if err := AtomicWrite(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return Request{Options: config.Options{RepoRoot: root, MachinePath: filepath.Join(t.TempDir(), "config.toml")}, StateDir: t.TempDir(), Path: path}, data
}
func runSave(t *testing.T, req Request) (Result, error) {
	t.Helper()
	r := NewResult(req.Machine, req.DryRun)
	err := Save(context.Background(), req, &r)
	return r, err
}
func TestCheckBeforeSave(t *testing.T) {
	t.Run("removing needed step", func(t *testing.T) {
		req, before := saveFixture(t)
		req.Edits = []Edit{{Path: []string{"steps", "build"}, Unset: true}}
		res, err := runSave(t, req)
		var se *SaveError
		if !errors.As(err, &se) || se.Code != "check_failed" {
			t.Fatalf("result %+v err %v", res, err)
		}
		found := false
		for _, p := range res.Problems {
			if p.Key == "steps.review.on_reject" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing on_reject diagnostic: %+v", res)
		}
		after, _ := Read(req.Path)
		if string(after) != string(before) {
			t.Fatal("refused save wrote file")
		}
	})
	t.Run("fixing one of two", func(t *testing.T) {
		req, b := saveFixture(t)
		b = []byte(strings.Replace(string(b), "max_parallel = 1", "max_parallel = 0", 1))
		b = append(b, []byte("\n[board]\ncolumns = \"invalid\"\n")...)
		if err := AtomicWrite(req.Path, b, 0644); err != nil {
			t.Fatal(err)
		}
		req.Edits = []Edit{{Path: []string{"max_parallel"}, Value: int64(3)}}
		res, err := runSave(t, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Problems) != 1 || res.Problems[0].Key != "board.columns" || res.Problems[0].New {
			t.Fatalf("diagnostics %+v", res)
		}
		after, _ := Read(req.Path)
		if !strings.Contains(string(after), "max_parallel = 3") {
			t.Fatal("not saved")
		}
	})
	t.Run("allow list rollback", func(t *testing.T) {
		req, b := saveFixture(t)
		b = append(b, []byte("\n[overrides]\nagents = [\"claude\", \"codex\"]\n")...)
		if err := AtomicWrite(req.Path, b, 0644); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(req.Options.RepoRoot, "openspec", "changes", "add-login")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tuckerbox.toml"), []byte("[steps.build]\nagent = \"codex\"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		req.Edits = []Edit{{Path: []string{"overrides", "agents"}, Value: []string{"claude"}}}
		res, err := runSave(t, req)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range res.Changes {
			if p.Change == "add-login" && p.Key == "steps.build.agent" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing change warning: %+v", res)
		}
	})
}
func TestSaveRevisionAndDryRun(t *testing.T) {
	req, b := saveFixture(t)
	req.IfMatch = Revision(b)
	req.Edits = []Edit{{Path: []string{"max_parallel"}, Value: int64(3)}, {Path: []string{"steps", "review", "max_rounds"}, Value: int64(2)}}
	req.DryRun = true
	if _, err := runSave(t, req); err != nil {
		t.Fatal(err)
	}
	after, _ := Read(req.Path)
	if string(after) != string(b) {
		t.Fatal("dry run wrote")
	}
	req.DryRun = false
	if _, err := runSave(t, req); err != nil {
		t.Fatal(err)
	}
	after, _ = Read(req.Path)
	if !strings.Contains(string(after), "max_parallel = 3") || !strings.Contains(string(after), "max_rounds = 2") {
		t.Fatalf("missing edits: %s", after)
	}
	req.Edits = []Edit{{Path: []string{"max_parallel"}, Value: int64(9)}}
	_, err := runSave(t, req)
	var se *SaveError
	if !errors.As(err, &se) || se.Code != "changed_since_read" {
		t.Fatalf("stale save: %v", err)
	}
	now, _ := Read(req.Path)
	if string(now) != string(after) {
		t.Fatal("stale save wrote")
	}
}
func TestMachineSaveAndUnset(t *testing.T) {
	req, b := saveFixture(t)
	req.Machine = true
	req.Path = req.Options.MachinePath
	req.IfMatch = "none"
	req.Edits = []Edit{{Path: []string{"steps", "review", "effort"}, Value: "high"}}
	if _, err := runSave(t, req); err != nil {
		t.Fatal(err)
	}
	after, _ := Read(filepath.Join(req.Options.RepoRoot, config.RepoFile))
	if string(after) != string(b) {
		t.Fatal("machine save wrote repo")
	}
	req.IfMatch = ""
	req.Edits[0].Unset = true
	if _, err := runSave(t, req); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(req.Options)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := cfg.Step("review")
	if s.Effort == "high" {
		t.Fatal("unset did not restore default")
	}
}
func TestCredentialsWrite(t *testing.T) {
	req, b := saveFixture(t)
	req.Machine = true
	req.Credentials = true
	req.Path = filepath.Join(filepath.Dir(req.Options.MachinePath), "credentials.toml")
	req.Edits = []Edit{{Path: []string{"forges", "github.com", "token"}, Value: "do-not-show-this-token"}}
	res, err := runSave(t, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Message, "do-not-show") {
		t.Fatal("leak")
	}
	st, err := os.Stat(req.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	if _, err = os.Stat(req.Options.MachinePath); !os.IsNotExist(err) {
		t.Fatal("machine config unexpectedly written")
	}
	repo, _ := Read(filepath.Join(req.Options.RepoRoot, config.RepoFile))
	if string(repo) != string(b) {
		t.Fatal("repo changed")
	}
	if runtime.GOOS != "windows" {
		if err = os.Chmod(req.Path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err = runSave(t, req); err == nil {
			t.Fatal("accepted nonprivate credentials")
		}
		os.Chmod(req.Path, 0600)
	}
	req.Edits[0].Unset = true
	if _, err = runSave(t, req); err != nil {
		t.Fatal(err)
	}
	data, _ := Read(req.Path)
	if strings.Contains(string(data), "do-not-show") {
		t.Fatal("unset retained token")
	}
}
func TestConcurrentStaleSaves(t *testing.T) {
	req, b := saveFixture(t)
	req.IfMatch = Revision(b)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := req
			r.Edits = []Edit{{Path: []string{"max_parallel"}, Value: int64(3 + i)}}
			res := NewResult(false, false)
			errs <- Save(context.Background(), r, &res)
		}(i)
	}
	wg.Wait()
	close(errs)
	saved, refused := 0, 0
	for err := range errs {
		if err == nil {
			saved++
		} else {
			var se *SaveError
			if errors.As(err, &se) && se.Code == "changed_since_read" {
				refused++
			} else {
				t.Fatal(err)
			}
		}
	}
	if saved != 1 || refused != 1 {
		t.Fatalf("saved %d refused %d", saved, refused)
	}
}
