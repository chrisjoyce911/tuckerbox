package cli

import (
	"bytes"
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
)

func TestShippingStatusSnapshot(t *testing.T) {
	tools.Setup(t)
	repo := t.TempDir()
	st := statedir.Dir(filepath.Join(repo, "state"))
	lock, err := runs.TryChangeLock(st.RunLock())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	r := &runs.Run{Record: ship.Record{ShippingStarted: true, MR: &ship.MRInfo{Link: "https://github.com/example/shop/pull/12", Number: 12, State: "open"}, CI: &ship.CIInfo{State: "pending", Commit: "abc123"}, ShipRound: 2}, Change: "add-login", Repo: repo, Worktree: repo, Branch: "add-login", State: runs.Shipping, Round: 2, Since: time.Now(), Created: time.Now()}
	if err := runs.WriteRun(st.RunFile(), r); err != nil {
		t.Fatal(err)
	}
	stateHome, err := runs.SystemStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := runs.Remember(stateHome, runs.IndexEntry{Repo: repo, Change: r.Change, Worktree: repo, State: string(st)}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	app := App{Stdout: &out, Stderr: &stderr}
	if code := app.Run([]string{"status", "--json"}); code != 0 {
		t.Fatal(stderr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
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
	out.Reset()
	stderr.Reset()
	code := app.Run([]string{"status"})
	for _, want := range []string{"shipping", "https://github.com/example/shop/pull/12", "pending", "SHIP ROUND"} {
		if code != 0 || !strings.Contains(out.String(), want) {
			t.Fatal(out.String(), stderr.String())
		}
	}
}
