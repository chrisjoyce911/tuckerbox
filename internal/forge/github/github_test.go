package github

import (
	"context"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/forgetest"
)

func client(t *testing.T, file string) *Client {
	t.Helper()
	c, err := New(forge.Target{Host: "github.com", Project: "sam/app"}, forgetest.Replay(t, "testdata/"+file, ""))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestRESTReplay(t *testing.T) {
	ctx := context.Background()
	c := client(t, "rest.json")
	a, err := c.Access(ctx)
	if err != nil || !a.Read || !a.Push || a.DefaultBranch != "main" {
		t.Fatal(a, err)
	}
	mr, err := c.FindMR(ctx, "add-login")
	if err != nil || mr != nil {
		t.Fatal(mr, err)
	}
	mr, err = c.OpenMR(ctx, forge.OpenRequest{Title: "add login", Body: "change=add-login", Branch: "add-login", Base: "main", Labels: []string{"automation"}})
	if err != nil || mr.Number != 12 || mr.Head != "abc123" || mr.State != "open" {
		t.Fatal(mr, err)
	}
	mr, err = c.GetMR(ctx, 12)
	if err != nil || !mr.Mergeable {
		t.Fatal(mr, err)
	}
	if err := c.Merge(ctx, 12, "abc123", "squash"); err != nil {
		t.Fatal(err)
	}
	mr, err = c.GetMR(ctx, 12)
	if err != nil || mr.State != "merged" {
		t.Fatal(mr, err)
	}
}
func TestChecksReplay(t *testing.T) {
	ctx := context.Background()
	c := client(t, "checks.json")
	checks, err := c.Checks(ctx, "abc123", "main")
	if err != nil || len(checks.Jobs) != 4 || len(checks.Required) != 2 {
		t.Fatal(checks, err)
	}
	for i, want := range []string{"green", "red", "pending", "green"} {
		if checks.Jobs[i].State != want {
			t.Fatal(checks)
		}
	}
	log, err := c.Log(ctx, checks.Jobs[1])
	if err != nil || log.Text != "test failed\n" {
		t.Fatal(log, err)
	}
	fallback, err := c.Log(ctx, forge.Job{Summary: "external summary", URL: "https://ci.example/job"})
	if err != nil || !strings.Contains(fallback.Text, "external summary") {
		t.Fatal(fallback, err)
	}
}
func TestMissingPinDoesNotSendRequest(t *testing.T) {
	c := &Client{}
	if c.Merge(context.Background(), 1, "", "merge") == nil {
		t.Fatal("missing SHA accepted")
	}
}

func TestReviewPaginationAndReplyWithoutResolving(t *testing.T) {
	ctx := context.Background()
	c := client(t, "review.json")
	threads, err := c.Threads(ctx, 12)
	if err != nil || len(threads) != 3 {
		t.Fatal(threads, err)
	}
	if threads[0].File != "internal/app/login.go" || threads[0].Line != 12 || threads[0].Resolved || len(threads[0].Comments) != 2 || !threads[0].Comments[1].Writer {
		t.Fatal(threads)
	}
	if threads[1].Comments[0].Writer || !threads[2].Resolved {
		t.Fatal(threads)
	}
	if err := c.Reply(ctx, 12, "thread1", "Fixed in def456: validation added"); err != nil {
		t.Fatal(err)
	}
}
