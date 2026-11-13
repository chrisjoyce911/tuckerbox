package team

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"path/filepath"
	"testing"
	"time"
)

type claimsForge struct {
	forge.TeamClient
	calls int
	err   error
}

func (f *claimsForge) ListOpenMRs(context.Context) ([]forge.MR, error) {
	f.calls++
	return []forge.MR{{Number: 1, Author: "alex", Body: "<!-- tuckerbox: change=login machine=studio run=1 by=sam -->"}}, f.err
}
func TestBoardReadTwiceAndOfflineAge(t *testing.T) {
	ctx := context.Background()
	f := &claimsForge{}
	p := filepath.Join(t.TempDir(), "claims.json")
	now := time.Now()
	one, err := Claims(ctx, f, p, now)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Claims(ctx, f, p, now.Add(30*time.Second))
	if err != nil || f.calls != 1 || one.Claims["login"].Login != "alex" || two.Claims["login"] != one.Claims["login"] {
		t.Fatal(one, two, f.calls, err)
	}
	f.err = fmt.Errorf("offline")
	stale, err := Claims(ctx, f, p, now.Add(2*time.Minute))
	if err != nil || !stale.Stale || stale.Err != "offline" || !stale.FetchedAt.Equal(one.FetchedAt) || stale.Claims["login"].Login != "alex" {
		t.Fatal(stale, err)
	}
	_, _ = Claims(ctx, f, p, now.Add(130*time.Second))
	if f.calls != 2 {
		t.Fatal(f.calls)
	}
}
