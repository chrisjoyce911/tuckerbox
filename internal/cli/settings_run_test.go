package cli

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/settings"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// change-settings, scenario Machines in solo mode: a change that prefers
// machine mini runs on studio when the user runs it there; the preference
// is kept for display and placement, and passes tuckerbox check without
// an allow-list.
func TestMachinesInSoloMode(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"fix-login"}})
	testenv.WriteFile(t, filepath.Join(f.env.Home, "config", "tuckerbox", "config.toml"), "machine = \"studio\"\n")
	testenv.WriteFile(t, filepath.Join(f.repo, "openspec", "changes", "fix-login", "tuckerbox.toml"), "machines = [\"mini\"]\n")
	f.commit("prefer mini")

	if res := f.tb("check"); res.code != 0 {
		t.Fatalf("check: exit %d: %s", res.code, res.stderr)
	}
	sc, ps, err := settings.Read(f.repo, "fix-login")
	if err != nil || len(ps) != 0 || !slices.Equal(sc.Machines, []string{"mini"}) {
		t.Fatalf("sidecar: %+v %v %v", sc, ps, err)
	}
	res := f.tb("run", "--foreground", "fix-login")
	if res.code != 0 {
		t.Fatalf("run: exit %d\n%s\n%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "step build round 1") {
		t.Errorf("build did not start:\n%s", res.stdout)
	}
	if s, _ := f.state("fix-login"); s != "ready" {
		t.Errorf("state %s", s)
	}
}
