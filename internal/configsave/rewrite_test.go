package configsave

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRewrite(t *testing.T) {
	tests := []struct {
		name, in string
		edit     Edit
		want     string
	}{
		{"comment", "# review\n[steps.review]\neffort = \"low\" # retained\nmodel = \"abc\"\n", Edit{Path: []string{"steps", "review", "effort"}, Value: "high"}, "# review\n[steps.review]\neffort = \"high\" # retained\nmodel = \"abc\"\n"},
		{"array", "[workflow]\nsteps = [\n  \"build\", # here\n  \"review\",\n] # order\n# end\n", Edit{Path: []string{"workflow", "steps"}, Value: []string{"review", "build"}}, "[workflow]\nsteps = [\"review\", \"build\"] # order\n# end\n"},
		{"remove step", "# keep\n[steps.build]\nagent = \"codex\"\n[steps.review]\nagent = \"claude\"\n", Edit{Path: []string{"steps", "build"}, Unset: true}, "# keep\n[steps.review]\nagent = \"claude\"\n"},
		{"remove last", "[steps.build]\n# keep\nagent = \"codex\"\n", Edit{Path: []string{"steps", "build", "agent"}, Unset: true}, "# keep\n"},
		{"untouched empty table", "max_parallel = 1\n[steps.empty]\n# keep\n", Edit{Path: []string{"max_parallel"}, Unset: true}, "[steps.empty]\n# keep\n"},
		{"quoted host", "[forges.\"git.example.org\"]\nkind = \"gitlab\"\n", Edit{Path: []string{"forges", "git.example.org", "token"}, Value: "secret"}, "[forges.\"git.example.org\"]\nkind = \"gitlab\"\ntoken = \"secret\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Rewrite([]byte(tt.in), []Edit{tt.edit})
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Fatalf("got %q want %q", out, tt.want)
			}
		})
	}
}
func TestRewriteUnusualLayoutRefused(t *testing.T) {
	_, err := Rewrite([]byte("steps.build = { agent = \"codex\" }\n"), []Edit{{Path: []string{"steps", "build", "agent"}, Value: "claude"}})
	if err == nil {
		t.Fatal("must refuse unsupported inline layout")
	}
}
func TestLockedReadCheckWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := AtomicWrite(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- WithFileLock(context.Background(), dir, path, func() error {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				return AtomicWrite(path, append(b, 'x'), 0600)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	if string(b) != strings.Repeat("x", 21) {
		t.Fatalf("lost updates: %s", b)
	}
}
