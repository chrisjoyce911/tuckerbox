package board

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

// fakeFleet is a fleet ref: what a fetch would bring, and the copy last
// fetched. fail makes a fetch fail (the remote can't be reached).
type fakeFleet struct {
	remote, local map[string]FleetRecord
	fetches       int
	fail          error
}

func (f *fakeFleet) Fetch(context.Context) (map[string]FleetRecord, error) {
	f.fetches++
	if f.fail != nil {
		return nil, f.fail
	}
	f.local = f.remote
	return f.remote, nil
}
func (f *fakeFleet) Local(context.Context) (map[string]FleetRecord, error) { return f.local, nil }

// modeRepo is a repository in mode with a remote and the changes given.
// top is more top-level config; tables go after it.
func modeRepo(t *testing.T, mode, top, tables string, changes ...string) string {
	t.Helper()
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: changes, Extra: "\n" + tables})
	p := filepath.Join(root, config.RepoFile)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(top, "ship =") {
		text = strings.Replace(text, "ship = \"local\"\n", "", 1)
	}
	testenv.WriteFile(t, p, "mode = '"+mode+"'\n"+top+text)
	testenv.Git(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "remote.git"))
	return root
}

func withFleet(t *testing.T, f *fakeFleet, at time.Time) {
	t.Helper()
	oldF, oldNow := FleetClaims, now
	FleetClaims = func(string, *config.Config) FleetSource { return f }
	now = func() time.Time { return at }
	t.Cleanup(func() { FleetClaims, now = oldF, oldNow })
}

func TestFleetBoard(t *testing.T) {
	root := modeRepo(t, "fleet", "", "[fleet]\nmax_runs = 3\n", "add-login", "add-search", "add-users")
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	f := &fakeFleet{remote: map[string]FleetRecord{
		"add-login":  {Change: "add-login", Machine: "studio", Run: "r1", State: "active", Epoch: 1, RenewedAt: at.Add(-time.Minute)},
		"add-search": {Change: "add-search", Machine: "mini", Run: "r2", State: "parked", Epoch: 2, RenewedAt: at.Add(-time.Hour)},
	}}
	withFleet(t, f, at)
	d := read(t, root, "")
	if d.Mode != "fleet" || d.Fleet == nil || d.Fleet.MaxRuns != 3 || d.Fleet.Claims != 2 {
		t.Fatalf("mode %q fleet %+v", d.Mode, d.Fleet)
	}
	if d.ClaimsRead == nil || d.ClaimsRead.Source != "fleet" || d.ClaimsRead.Stale {
		t.Fatalf("claims read %+v", d.ClaimsRead)
	}
	if c := card(t, d, "add-login").Claim; c == nil || c.Machine != "studio" || c.State != "active" || c.Epoch != 1 {
		t.Fatalf("add-login claim %+v", c)
	}
	// Parked claims don't expire, however old.
	if c := card(t, d, "add-search").Claim; c == nil || c.State != "parked" {
		t.Fatalf("add-search claim %+v", c)
	}
	if c := card(t, d, "add-users").Claim; c != nil {
		t.Fatalf("unclaimed card has claim %+v", c)
	}
	// A ready claim neither counts nor expires.
	f.remote = map[string]FleetRecord{"add-users": {Change: "add-users", Machine: "mini", State: "ready", Epoch: 1, RenewedAt: at.Add(-time.Hour)}}
	now = func() time.Time { return at.Add(2 * time.Minute) } // past the fetch interval
	d = read(t, root, "")
	if d.Fleet.Claims != 0 || card(t, d, "add-users").Claim.State != "ready" || f.fetches != 2 {
		t.Fatalf("ready: fleet %+v claim %+v fetches %d", d.Fleet, card(t, d, "add-users").Claim, f.fetches)
	}
	// The document's field names (board-data: additions within version 1).
	data, _ := json.Marshal(d)
	for _, want := range []string{`"mode":"fleet"`, `"fleet":{"max_runs":3,"claims":0}`, `"claim":{"machine":"mini","state":"ready","epoch":1,"renewed_at":`, `"claims_read":{"source":"fleet","at":`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("document lacks %s", want)
		}
	}
}

func TestFleetBoardFetchesOnceAMinute(t *testing.T) {
	root := modeRepo(t, "fleet", "", "", "add-login")
	at := time.Now()
	f := &fakeFleet{remote: map[string]FleetRecord{"add-login": {Change: "add-login", Machine: "studio", State: "active", Epoch: 1, RenewedAt: at}}}
	withFleet(t, f, at)
	read(t, root, "")
	now = func() time.Time { return at.Add(30 * time.Second) }
	if c := card(t, read(t, root, ""), "add-login").Claim; c == nil || f.fetches != 1 {
		t.Fatalf("second read fetched again (%d) or lost the claim %+v", f.fetches, c)
	}
	// The remote can't be reached: the copy last fetched, said to be stale.
	now = func() time.Time { return at.Add(90 * time.Second) }
	f.fail = errors.New("remote unreachable")
	d := read(t, root, "")
	if !d.ClaimsRead.Stale || d.ClaimsRead.Error == "" || d.ClaimsRead.At == "" || card(t, d, "add-login").Claim == nil {
		t.Fatalf("offline read %+v claim %+v", d.ClaimsRead, card(t, d, "add-login").Claim)
	}
}

// Expired claim on a card: add-login's claim on mini wasn't renewed within
// the lease, and mini isn't connected (no run of it here): its card still
// shows the claim, expired, with mini and the last renewal.
func TestExpiredClaimOnACard(t *testing.T) {
	root := modeRepo(t, "fleet", "", "[fleet]\nlease = '10m'\n", "add-login")
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	renewed := at.Add(-12 * time.Minute)
	withFleet(t, &fakeFleet{remote: map[string]FleetRecord{"add-login": {Change: "add-login", Machine: "mini", Run: "r1", State: "active", Epoch: 1, RenewedAt: renewed}}}, at)
	c := card(t, read(t, root, ""), "add-login")
	if c.Claim == nil || c.Claim.State != "expired" || c.Claim.Machine != "mini" || c.Claim.RenewedAt != timestamp(renewed) || c.State != "" {
		t.Fatalf("card %+v claim %+v", c, c.Claim)
	}
	// Within the lease and its minute of skew it is still active.
	if s := fleetState(FleetRecord{State: "active", RenewedAt: at.Add(-10*time.Minute - 30*time.Second)}, 10*time.Minute, at); s != "active" {
		t.Fatal(s)
	}
}

// Owner in a team: login sam holds add-login from machine studio.
func TestOwnerInATeam(t *testing.T) {
	root := modeRepo(t, "team", "ship = 'mr'\n", "", "add-login", "add-search")
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	old := TeamClaims
	t.Cleanup(func() { TeamClaims = old })
	TeamClaims = func(_ context.Context, r string, cfg *config.Config, _ time.Time) (TeamSnapshot, error) {
		if cfg.Mode != config.ModeTeam {
			t.Errorf("mode %q", cfg.Mode)
		}
		return TeamSnapshot{Claims: map[string]TeamClaim{"add-login": {Change: "add-login", Login: "sam", Machine: "studio", Run: "r9"}}, FetchedAt: at}, nil
	}
	teamRead := func() *Document { return teamRead(t, root) }
	d := teamRead()
	if d.Mode != "team" || d.Fleet != nil || d.ClaimsRead == nil || d.ClaimsRead.Source != "forge" {
		t.Fatalf("mode %q fleet %+v read %+v", d.Mode, d.Fleet, d.ClaimsRead)
	}
	if c := card(t, d, "add-login").Claim; c == nil || c.Login != "sam" || c.Machine != "studio" || c.State != "open" {
		t.Fatalf("claim %+v", c)
	}
	if c := card(t, d, "add-search").Claim; c != nil {
		t.Fatalf("unclaimed %+v", c)
	}
	// The forge is down: the kept answer, with its age.
	TeamClaims = func(context.Context, string, *config.Config, time.Time) (TeamSnapshot, error) {
		return TeamSnapshot{Claims: map[string]TeamClaim{"add-login": {Change: "add-login", Login: "sam", Machine: "studio"}}, FetchedAt: at, Stale: true, Err: "forge unreachable"}, nil
	}
	d = teamRead()
	if !d.ClaimsRead.Stale || d.ClaimsRead.At != timestamp(at) || card(t, d, "add-login").Claim.Login != "sam" {
		t.Fatalf("stale %+v", d.ClaimsRead)
	}
}

// teamRead reads a team repository's board as if its remote were on a forge.
func teamRead(t *testing.T, root string) *Document {
	t.Helper()
	env := config.SystemEnv()
	realGit := env.Git
	env.Git = func(dir string, args ...string) (string, error) {
		if a := strings.Join(args, " "); a == "config --get remote.origin.url" || a == "remote get-url --push -- origin" {
			return "https://github.com/example/shop.git", nil
		}
		return realGit(dir, args...)
	}
	d, err := Read(context.Background(), root, "", env)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// team-claims 5.30: in a team repository the board's approval ignores
// refs/tuckerbox/approvals; only tuckerbox.toml counts.
func TestTeamBoardIgnoresApprovalsRef(t *testing.T) {
	ctx := context.Background()
	root := modeRepo(t, "team", "ship = 'mr'\n", "", "add-login")
	old := TeamClaims
	t.Cleanup(func() { TeamClaims = old })
	TeamClaims = func(context.Context, string, *config.Config, time.Time) (TeamSnapshot, error) {
		return TeamSnapshot{}, nil
	}
	hash, err := approval.Hash(approval.DirOf(root, "add-login"))
	if err != nil {
		t.Fatal(err)
	}
	// The remote is gone: the entry stays in the local ref, unpublished.
	if err := (approval.Store{Dir: root}).Record(ctx, "add-login", approval.Entry{State: approval.EntryApproved, Hash: hash, By: "Owner", At: time.Now()}); !errors.Is(err, approval.ErrOffline) {
		t.Fatalf("Record = %v", err)
	}
	if a := card(t, teamRead(t, root), "add-login").Approval; a.State != "none" || !a.Published {
		t.Fatalf("team card approval from the ref: %+v", a)
	}
}

// A run made while the repository was solo, after it became a fleet
// repository: the board is a fleet board, the run's card has no claim and
// still shows its run, and Dispatch doesn't count it (it has a run).
func TestRunFromBeforeFleetMode(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	worktree := filepath.Join(filepath.Dir(root), "repo-worktrees", "add-login")
	testenv.Git(t, root, "worktree", "add", "-qb", "add-login", worktree)
	st, err := statedir.Resolve(context.Background(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.WriteRun(st.RunFile(), &runs.Run{Change: "add-login", State: runs.Stopped, Step: "build", Round: 1, Branch: "add-login", Worktree: worktree, Since: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if d := read(t, root, ""); d.Mode != "solo" || card(t, d, "add-login").State != "stopped" {
		t.Fatalf("solo %q %+v", d.Mode, card(t, d, "add-login"))
	}
	p := filepath.Join(root, config.RepoFile)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	testenv.WriteFile(t, p, "mode = 'fleet'\n"+string(data))
	testenv.Git(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "remote.git"))
	withFleet(t, &fakeFleet{remote: map[string]FleetRecord{}}, time.Now())
	d := read(t, root, "")
	c := card(t, d, "add-login")
	if d.Mode != "fleet" || d.Fleet.Claims != 0 || c.Claim != nil || c.State != "stopped" {
		t.Fatalf("fleet %q %+v card %+v", d.Mode, d.Fleet, c)
	}
}

func TestSoloBoardHasNoClaims(t *testing.T) {
	testTools.Setup(t)
	root := testTools.NewRepo(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	d := read(t, root, "")
	if d.Mode != "solo" || d.Fleet != nil || d.ClaimsRead != nil || card(t, d, "add-login").Claim != nil {
		t.Fatalf("solo %+v", d)
	}
}
