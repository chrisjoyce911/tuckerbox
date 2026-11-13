package ship

import (
	"context"
	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type ownedForge struct {
	*fakeForge
	login string
}

func (f ownedForge) OpenMR(ctx context.Context, r forge.OpenRequest) (*forge.MR, error) {
	m, err := f.fakeForge.OpenMR(ctx, r)
	if err == nil {
		f.mu.Lock()
		f.mr.Author = f.login
		m.Author = f.login
		f.mu.Unlock()
	}
	return m, err
}
func TestTwoPeopleTeamClaimAndCopiedMarker(t *testing.T) {
	f := repositories(t)
	api := &fakeForge{}
	claims := []*Claim{f.claim(f.one, "studio"), f.claim(f.two, "laptop")}
	claims[0].Identity.By = "sam"
	claims[1].Identity.By = "alex"
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, c := range claims {
		wg.Add(1)
		go func(i int, c *Claim) {
			defer wg.Done()
			_, errs[i] = c.Acquire(context.Background(), ownedForge{api, c.Identity.By}, forge.OpenRequest{Draft: true}, func(sha string) error {
				return os.WriteFile(filepath.Join(c.Git.Worktree, ".git", "team-lease"), []byte(sha), 0600)
			})
		}(i, c)
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatal(errs)
	}
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if api.opens != 1 {
		t.Fatal(api.opens)
	}
	c := claims[winner]
	if err := c.Check(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.mr.Author = "someone-else"
	api.mu.Unlock()
	if err := c.Check(context.Background(), api); err == nil || !strings.Contains(err.Error(), "claimed by someone-else") {
		t.Fatal(err)
	}
}

type closesDuringPushForge struct {
	*fakeForge
	reads int
}

func (f *closesDuringPushForge) FindMR(ctx context.Context, b string) (*forge.MR, error) {
	f.reads++
	if f.reads > 1 {
		return nil, nil
	}
	return f.fakeForge.FindMR(ctx, b)
}
func TestTeamShippingNeverReopensClosedDraft(t *testing.T) {
	f := repositories(t)
	c := f.claim(f.one, "studio")
	c.Identity.By = "sam"
	api := &fakeForge{}
	owned := ownedForge{api, "sam"}
	_, err := c.Acquire(context.Background(), owned, forge.OpenRequest{Draft: true}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	gitTest(t, f.one, "commit", "--allow-empty", "-m", "finished steps")
	closing := &closesDuringPushForge{fakeForge: api}
	_, err = c.PushTeam(context.Background(), closing, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "merge request closed") || api.opens != 1 {
		t.Fatal(err, api.opens)
	}
}

type closedOnLaterReadForge struct {
	*fullForge
	reads, closeAfter int
}

func (f *closedOnLaterReadForge) FindMR(ctx context.Context, b string) (*forge.MR, error) {
	f.reads++
	if f.reads > f.closeAfter {
		return nil, nil
	}
	return f.fullForge.FindMR(ctx, b)
}
func TestTeamFixAndCITickNeverReopenClosedClaim(t *testing.T) {
	for _, scenario := range []string{"fix", "CI tick"} {
		t.Run(scenario, func(t *testing.T) {
			e, api, f := stage(t, config.ShipMR)
			e.Identity.By = "sam"
			claim := &Claim{Identity: e.Identity, Git: e.Git}
			mr, err := claim.Acquire(context.Background(), ownedForge{api.fakeForge, "sam"}, forge.OpenRequest{Draft: true}, e.persistPush)
			if err != nil {
				t.Fatal(err)
			}
			e.Record.MR = info(mr)
			e.Record.CI = &CIInfo{State: "red"}
			e.Forge = &closedOnLaterReadForge{fullForge: api, closeAfter: 1}
			var out Outcome
			if scenario == "fix" {
				e.Fix = func(context.Context, string, []agent.Finding) (*agent.Result, error) {
					gitTest(t, f.one, "commit", "--allow-empty", "-m", "review fix")
					return &agent.Result{Status: agent.Done, Summary: "fixed"}, nil
				}
				out = e.fix(context.Background(), claim, Feedback{Step: "fix_ci"})
			} else {
				writeTasks(t, filepath.Join(f.one, "openspec", "changes", "add-login", "tasks.md"), ciTasks)
				var msgs []string
				e.Commit = gitCommit(t, f.one, &msgs)
				_, out = e.tickCI(context.Background(), claim)
			}
			if out.State != "blocked" || !strings.Contains(out.Reason, "merge request closed") || api.opens != 1 {
				t.Fatal(out, api.opens)
			}
		})
	}
}

type behindTeamForge struct{ *closedOnLaterReadForge }

func (f *behindTeamForge) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	m, err := f.fullForge.GetMR(ctx, n)
	if err == nil {
		m.Behind = true
	}
	return m, err
}
func TestTeamBaseUpdateNeverReopensClosedClaim(t *testing.T) {
	e, api, f := stage(t, config.ShipMerge)
	e.Identity.By = "sam"
	claim := &Claim{Identity: e.Identity, Git: e.Git}
	mr, err := claim.Acquire(context.Background(), ownedForge{api.fakeForge, "sam"}, forge.OpenRequest{Draft: true}, e.persistPush)
	if err != nil {
		t.Fatal(err)
	}
	e.Record.MR = info(mr)
	gitTest(t, f.two, "checkout", "main")
	writeTest(t, f.two, "newbase.txt", "base changed")
	gitTest(t, f.two, "add", ".")
	gitTest(t, f.two, "commit", "-m", "new base")
	gitTest(t, f.two, "push", "origin", "main")
	e.Forge = &behindTeamForge{&closedOnLaterReadForge{fullForge: api, closeAfter: 0}}
	out := e.watch(context.Background(), claim)
	if out.State != "blocked" || !strings.Contains(out.Reason, "merge request closed") || api.opens != 1 {
		t.Fatal(out, api.opens)
	}
}
