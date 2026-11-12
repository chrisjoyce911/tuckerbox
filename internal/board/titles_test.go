package board

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
)

// repoChanges is this repository's openspec/changes folder.
const repoChanges = "../../openspec/changes"

// untitledChanges names each active change under changes (not archive/)
// whose tuckerbox.toml has no non-empty title.
func untitledChanges(t *testing.T, changes string) []string {
	t.Helper()
	entries, err := os.ReadDir(changes)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "archive" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		var side struct {
			Title string `toml:"title"`
		}
		data, err := os.ReadFile(filepath.Join(changes, e.Name(), approval.SidecarName))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		if _, err := toml.Decode(string(data), &side); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if strings.TrimSpace(side.Title) == "" {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Requirement: Titles of this repository's changes. The proposal's heading
// is never used, so every active change carries a title in tuckerbox.toml.
func TestRepositoryChangesHaveTitles(t *testing.T) {
	if missing := untitledChanges(t, repoChanges); len(missing) > 0 {
		t.Errorf("changes without a title in %s: %s (add title = \"...\")", approval.SidecarName, strings.Join(missing, ", "))
	}
}

// Scenario: Change added without a title.
func TestChangeAddedWithoutATitleIsNamed(t *testing.T) {
	changes := t.TempDir()
	for name, sidecar := range map[string]string{"titled": `title = "Titled"`, "untitled": `depends_on = []`, "blank": `title = "  "`, "no-sidecar": ""} {
		dir := filepath.Join(changes, name)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "proposal.md"), []byte("# Proposal\n"), 0o644)
		if sidecar != "" {
			os.WriteFile(filepath.Join(dir, approval.SidecarName), []byte(sidecar+"\n"), 0o644)
		}
	}
	os.MkdirAll(filepath.Join(changes, "archive", "2026-01-01-old"), 0o755)
	if got := strings.Join(untitledChanges(t, changes), ","); got != "blank,no-sidecar,untitled" {
		t.Errorf("named %q", got)
	}
}

// Scenario: Herdr board change.
func TestHerdrBoardChangeTitle(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join(repoChanges, "add-herdr-board"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := readCard(context.Background(), "add-herdr-board", dir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Herdr board" {
		t.Errorf("title %q", c.Title)
	}
}
