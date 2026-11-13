package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func gitStdin(t *testing.T, dir, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// pushFleetClaim puts a fleet ref holding one claim on origin, as a fleet
// machine would have left it.
func pushFleetClaim(t *testing.T, repo, change, machine string) {
	t.Helper()
	blob := gitStdin(t, repo, "change = \""+change+"\"\nmachine = \""+machine+"\"\nepoch = 1\nstate = \"active\"\n", "hash-object", "-w", "--stdin")
	claims := gitStdin(t, repo, "100644 blob "+blob+"\t"+change+".toml\n", "mktree")
	root := gitStdin(t, repo, "040000 tree "+claims+"\tclaims\n", "mktree")
	commit := gitStdin(t, repo, "", "commit-tree", root, "-m", "claim "+change)
	testenv.Git(t, repo, "push", "-q", "origin", commit+":"+FleetRef)
}

func setMode(t *testing.T, repo, mode string) {
	t.Helper()
	path := filepath.Join(repo, ".tuckerbox", "config.toml")
	data := readFile(t, path)
	testenv.WriteFile(t, path, "mode = \""+mode+"\"\n"+data)
}

// Scenario: Fleet to solo with a claim left. check passes and lists the
// leftover claim with its machine.
func TestFleetToSoloWithClaimLeft(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	f.withRemote()
	pushFleetClaim(t, f.repo, "add-login", "studio")

	res := f.tb("check")
	if res.code != 0 {
		t.Fatalf("check: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "mode: solo") || !strings.Contains(res.stdout, "leftover fleet claim on add-login (studio)") {
		t.Fatalf("check output lacks the leftover claim:\n%s", res.stdout)
	}
	// check read the ref without writing one locally.
	if out := testenv.Git(t, f.repo, "for-each-ref", FleetRef); out != "" {
		t.Fatalf("check wrote %s locally: %s", FleetRef, out)
	}

	// In fleet mode the claim is the fleet's, not left over.
	setMode(t, f.repo, "fleet")
	res = f.tb("check")
	if res.code != 0 || strings.Contains(res.stdout, "leftover") || !strings.Contains(res.stdout, "mode: fleet") {
		t.Fatalf("check in fleet mode: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Fleet without a remote.
func TestCheckFleetWithoutRemote(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	setMode(t, f.repo, "fleet")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, "fleet mode needs a remote") {
		t.Fatalf("check: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Team with local shipping.
func TestCheckTeamWithLocalShipping(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[team]\napprovers = [\"sam\"]\n"})
	setMode(t, f.repo, "team")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, `team mode needs ship = "mr" or "merge"`) {
		t.Fatalf("check: exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
}

// Scenario: Machine config sets the mode, through check.
func TestCheckMachineConfigSetsMode(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	writeMachineConfig(t, f.env, "mode = \"fleet\"\n\n[fleet]\nmax_runs = 10\n")
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, "mode: mode belongs in repository config") || !strings.Contains(res.stderr, "fleet.max_runs: belongs in repository config") {
		t.Fatalf("check: exit %d\n%s", res.code, res.stderr)
	}
}

// Scenario: Repository lists machines, through check.
func TestCheckRepositoryListsMachines(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}, Extra: "\n[machines.mini]\nssh = \"mini\"\n"})
	res := f.tb("check")
	if res.code == 0 || !strings.Contains(res.stderr, "machines: machines belongs in machine config") {
		t.Fatalf("check: exit %d\n%s", res.code, res.stderr)
	}
}
