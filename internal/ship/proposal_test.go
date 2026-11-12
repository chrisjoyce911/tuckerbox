package ship

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

func TestProposalRequestTitleSectionsAndLimits(t *testing.T) {
	for _, archived := range []bool{false, true} {
		root := t.TempDir()
		folder := filepath.Join(root, "openspec", "changes", "add-login")
		if archived {
			folder = filepath.Join(root, "openspec", "changes", "archive", "2026-11-12-add-login")
		}
		if err := os.MkdirAll(folder, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "proposal.md"), []byte("# Proposal\n## Why\nLogin matters\n## What Changes\nAdd a form\n## Impact\nDO NOT INCLUDE\n"), 0644); err != nil {
			t.Fatal(err)
		}
		target := forge.Target{Kind: forge.GitHub, Host: "github.com", Project: "sam/app"}
		identity := Identity{"add-login", "studio", "run1"}
		r, err := ProposalRequest(root, "add-login", target, "add-login", "main", []string{"automation"}, identity)
		if err != nil || r.Title != "add login" || !strings.Contains(r.Body, "Login matters") || !strings.Contains(r.Body, "Add a form") || strings.Contains(r.Body, "DO NOT INCLUDE") {
			t.Fatal(r, err)
		}
		if err := os.WriteFile(filepath.Join(folder, "tuckerbox.toml"), []byte("title = 'Login for customers'\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "proposal.md"), []byte("## Why\n"+strings.Repeat("界", 30000)), 0644); err != nil {
			t.Fatal(err)
		}
		r, err = ProposalRequest(root, "add-login", target, "add-login", "main", nil, identity)
		marker, _ := identity.Marker()
		if err != nil || r.Title != "Login for customers" || len(r.Body)+len(marker)+2 > 65536 || !strings.HasSuffix(r.Body, "proposal.md)") || !utf8.ValidString(r.Body) {
			t.Fatal(r.Title, len(r.Body), err)
		}
		if archived && !strings.Contains(r.Body, "archive/2026-11-12-add-login") {
			t.Fatal("truncation link targets moved proposal")
		}
	}
}
