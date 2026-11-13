package github

import (
	"context"
	"testing"
)

func TestFindMergedMRIncludesClosedHistoryAndPages(t *testing.T) {
	c := client(t, "fleet-merged.json")
	m, err := c.FindMergedMR(context.Background(), "add-login")
	if err != nil || m == nil || m.Number != 9 || m.State != "merged" {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = c.FindMergedMR(context.Background(), "unmerged")
	if err != nil || m != nil {
		t.Fatalf("closed unmerged MR refused: %+v %v", m, err)
	}
}
