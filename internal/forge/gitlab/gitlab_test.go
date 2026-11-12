package gitlab

import (
	"context"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/forge/forgetest"
)

func client(t *testing.T, file string) *Client {
	t.Helper()
	c, err := New(forge.Target{Host: "gitlab.com", Project: "team/tools/app"}, forgetest.Replay(t, "testdata/"+file, "/api/v4"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestRESTReplaySubgroups(t *testing.T) {
	ctx := context.Background()
	c := client(t, "rest.json")
	a, err := c.Access(ctx)
	if err != nil || !a.Read || !a.Push {
		t.Fatal(a, err)
	}
	m, err := c.FindMR(ctx, "add-login")
	if err != nil || m != nil {
		t.Fatal(m, err)
	}
	m, err = c.OpenMR(ctx, forge.OpenRequest{Title: "add login", Body: "change=add-login", Branch: "add-login", Base: "main", Labels: []string{"automation"}})
	if err != nil || m.State != "open" || m.Number != 12 {
		t.Fatal(m, err)
	}
	m, err = c.GetMR(ctx, 12)
	if err != nil || !m.Mergeable {
		t.Fatal(m, err)
	}
	if err := c.Merge(ctx, 12, "abc123", "squash"); err != nil {
		t.Fatal(err)
	}
}
func TestNewestPipelineAndAllowedFailure(t *testing.T) {
	ctx := context.Background()
	c := client(t, "checks.json")
	checks, err := c.Checks(ctx, "abc123", "main")
	if err != nil || len(checks.Jobs) != 2 {
		t.Fatal(checks, err)
	}
	for _, j := range checks.Jobs {
		if j.State != "green" {
			t.Fatal(j)
		}
	}
	checks, err = c.Checks(ctx, "newhead", "main")
	if err != nil || checks.Jobs[0].State != "pending" {
		t.Fatal(checks, err)
	}
	checks, err = c.Checks(ctx, "badhead", "main")
	if err != nil || checks.Jobs[0].State != "red" {
		t.Fatal(checks, err)
	}
	log, err := c.Log(ctx, checks.Jobs[0])
	if err != nil || log.Text != "test failed\n" {
		t.Fatal(log, err)
	}
	checks, err = c.Checks(ctx, "noci", "main")
	if err != nil || len(checks.Jobs) != 0 {
		t.Fatal(checks, err)
	}
}
func TestUnsupportedRebaseAndMissingPinSendNothing(t *testing.T) {
	c := &Client{}
	if c.Merge(context.Background(), 1, "abc123", "rebase") == nil {
		t.Fatal("rebase accepted")
	}
	if c.Merge(context.Background(), 1, "", "merge") == nil {
		t.Fatal("missing SHA accepted")
	}
}

func TestReviewPaginationMembershipAndReply(t *testing.T) {
	ctx := context.Background()
	c := client(t, "review.json")
	threads, err := c.Threads(ctx, 12)
	if err != nil || len(threads) != 2 {
		t.Fatal(threads, err)
	}
	if threads[0].File != "internal/app/login.go" || threads[0].Line != 12 || threads[0].Resolved || len(threads[0].Comments) != 1 || !threads[0].Comments[0].Writer {
		t.Fatal(threads)
	}
	if threads[1].Comments[0].Writer {
		t.Fatal(threads)
	}
	if err := c.Reply(ctx, 12, "thread1", "Fixed in def456: validation added"); err != nil {
		t.Fatal(err)
	}
}
