package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
	"github.com/chrisjoyce911/tuckerbox/internal/testenv"
)

func TestShippingStatusSnapshot(t *testing.T) {
	f := newCLI(t, testenv.RepoOptions{Changes: []string{"add-login"}})
	st, err := statedir.Resolve(context.Background(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := runs.TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	r := &runs.Run{Record: ship.Record{ShippingStarted: true, MR: &ship.MRInfo{Link: "https://github.com/example/shop/pull/12", Number: 12, State: "open"}, CI: &ship.CIInfo{State: "pending", Commit: "abc123"}, ShipRound: 2}, Change: "add-login", Repo: f.repo, Worktree: f.repo, Branch: "add-login", State: runs.Shipping, Round: 2, Since: time.Now(), Created: time.Now()}
	if err := runs.WriteRun(st.RunFile(), r); err != nil {
		t.Fatal(err)
	}
	stateHome, err := runs.SystemStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.Remember(stateHome, runs.IndexEntry{Repo: f.repo, Change: r.Change, Worktree: f.repo, State: string(st)}); err != nil {
		t.Fatal(err)
	}
	result := f.tb("status", "--json")
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &doc); err != nil {
		t.Fatal(err)
	}
	rows := doc["runs"].([]any)
	if len(rows) != 1 {
		t.Fatal(doc)
	}
	row := rows[0].(map[string]any)
	extracted := map[string]any{"version": doc["version"], "run": map[string]any{"state": row["state"], "mr": row["mr"], "ci": row["ci"], "ship_round": row["ship_round"]}}
	data, err := os.ReadFile(filepath.Join("testdata", "status-shipping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(extracted, expected) {
		t.Fatal(extracted, expected)
	}
	table := f.tb("status")
	for _, want := range []string{"shipping", "https://github.com/example/shop/pull/12", "pending", "SHIP ROUND"} {
		if table.code != 0 || !strings.Contains(table.stdout, want) {
			t.Fatal(table)
		}
	}
}
