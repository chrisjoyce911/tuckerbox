package ship

import (
	"context"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
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
			_, errs[i] = c.Acquire(context.Background(), ownedForge{api, c.Identity.By}, forge.OpenRequest{Draft: true}, func(string) error { return nil })
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
