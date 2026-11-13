package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestTeamTakeoverRefused(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"login"}})
	testenv.Git(t, f.repo, "remote", "add", "origin", "https://github.com/example/shop.git")
	path := filepath.Join(f.repo, ".tuckerbox", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "ship = \"local\"", "ship = \"mr\"", 1)
	testenv.WriteFile(t, path, "mode = \"team\"\n"+text)
	res := f.tb("run", "--take-over", "login")
	if res.code == 0 || !strings.Contains(res.stderr, "close the merge request and delete its branch") {
		t.Fatalf("%d %s", res.code, res.stderr)
	}
}

func TestClaimShowAndRelease(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"login"}})
	bare := filepath.Join(t.TempDir(), "remote.git")
	testenv.Git(t, f.repo, "clone", "--bare", f.repo, bare)
	testenv.Git(t, f.repo, "remote", "add", "origin", bare)
	store := fleet.Store{Dir: f.repo}
	got, err := store.Acquire(context.Background(), "login", "other", "run", "login", false, false)
	if err != nil {
		t.Fatal(err)
	}
	res := f.tb("claim", "show", "--json")
	var list []fleet.Claim
	if res.code != 0 || json.Unmarshal([]byte(res.stdout), &list) != nil || len(list) != 1 || list[0].Machine != "other" {
		t.Fatalf("%+v: %+v", res, list)
	}
	res = f.tb("claim", "release", "login")
	if res.code == 0 || !strings.Contains(res.stderr, "--force") {
		t.Fatal(res)
	}
	res = f.tb("claim", "release", "--force", "login")
	if res.code != 0 {
		t.Fatal(res)
	}
	if err := store.Renew(context.Background(), got.Claim); err == nil || !strings.Contains(err.Error(), "claim released") {
		t.Fatal(err)
	}
}
