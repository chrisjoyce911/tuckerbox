package gitlab

import (
	"context"
	"testing"
)

func TestFindMergedMRUsesMergedHistory(t *testing.T) {
	c := client(t, "fleet-merged.json")
	m, err := c.FindMergedMR(context.Background(), "add-login")
	if err != nil || m == nil || m.Number != 9 || m.State != "merged" {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = c.FindMergedMR(context.Background(), "unmerged")
	if err != nil || m != nil {
		t.Fatalf("%+v %v", m, err)
	}
}
