package github

import (
	"context"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"strings"
	"testing"
)

func TestTeamDraftProtectionAndReady(t *testing.T) {
	c := client(t, "team.json")
	ctx := context.Background()
	login, err := c.Login(ctx)
	if err != nil || login != "sam" {
		t.Fatal(login, err)
	}
	ok, err := c.ProtectedBase(ctx, "main")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	mr, err := c.OpenMR(ctx, forge.OpenRequest{Title: "login", Body: "marker", Branch: "login", Base: "main", Draft: true})
	if err != nil || !mr.Draft || mr.Author != "sam" {
		t.Fatal(mr, err)
	}
	list, err := c.ListOpenMRs(ctx)
	if err != nil || len(list) != 1 || list[0].Author != "sam" {
		t.Fatal(list, err)
	}
	_, err = c.UpdateMR(ctx, 12, forge.OpenRequest{Title: "Login ready", Body: "proposal"})
	if err != nil {
		t.Fatal(err)
	}
	if err = c.MarkReady(ctx, 12); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ok, err = c.ProtectedBase(ctx, "main")
		if err != nil || ok {
			t.Fatal(ok, err)
		}
	}
	_, err = c.ProtectedBase(ctx, "main")
	if err == nil || !strings.Contains(err.Error(), "Administration: read") {
		t.Fatal(err)
	}
}
