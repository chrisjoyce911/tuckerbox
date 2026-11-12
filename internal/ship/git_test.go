package ship

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteOnlyUnmovedMergedBranch(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "person pushed"}[moved], func(t *testing.T) {
			f := repositories(t)
			c := f.claim(f.one, "studio")
			head, err := c.Git.Push(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if moved {
				humanFix(t, f)
			}
			deleted, err := c.Git.DeleteMerged(context.Background(), head)
			if err != nil || deleted == moved {
				t.Fatal(deleted, err)
			}
			if _, err := os.Stat(filepath.Join(f.one, "fix.txt")); err != nil {
				t.Fatal("local worktree removed", err)
			}
			tip, err := c.Git.Tip(context.Background())
			if err != nil || (tip == "") != deleted {
				t.Fatal(tip, deleted, err)
			}
		})
	}
}
func TestMergeBaseAndAbortConflicts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "conflict"}[conflict], func(t *testing.T) {
			f := repositories(t)
			c := f.claim(f.one, "studio")
			if conflict {
				writeTest(t, f.one, "base.txt", "change branch\n")
				gitTest(t, f.one, "add", ".")
				gitTest(t, f.one, "commit", "-m", "branch edits base")
			}
			before := gitTest(t, f.one, "rev-parse", "HEAD")
			gitTest(t, f.two, "checkout", "main")
			writeTest(t, f.two, "base.txt", "base branch\n")
			gitTest(t, f.two, "add", ".")
			gitTest(t, f.two, "commit", "-m", "base update")
			gitTest(t, f.two, "push", "origin", "main")
			base := gitTest(t, f.two, "rev-parse", "HEAD")
			after, err := c.Git.UpdateBase(context.Background())
			if conflict {
				if err == nil || !strings.Contains(err.Error(), "base.txt") {
					t.Fatal(err)
				}
				if gitTest(t, f.one, "rev-parse", "HEAD") != before || gitTest(t, f.one, "status", "--porcelain") != "" {
					t.Fatal("conflicted merge not aborted")
				}
			} else {
				if err != nil || after == before {
					t.Fatal(after, err)
				}
				gitTest(t, f.one, "merge-base", "--is-ancestor", before, after)
				gitTest(t, f.one, "merge-base", "--is-ancestor", base, after)
				if parents := gitTest(t, f.one, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(parents)) != 3 {
					t.Fatal("base update did not make a merge", parents)
				}
			}
		})
	}
}
