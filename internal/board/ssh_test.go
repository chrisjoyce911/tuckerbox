package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

func TestUnsafeSSHHostsNeverInvokeSSH(t *testing.T) {
	for _, remote := range []string{"git@a$(command):x/y", "git@a`command`:x/y", "a;command:x/y", "-option:x/y", "host name:x/y", "ssh://git@[::1]/x/y"} {
		want := config.NormalizeRemote(remote)
		got := repoKey(context.Background(), remote, func(context.Context, string) (string, error) {
			t.Fatalf("SSH called for unsafe remote %q", remote)
			return "", nil
		})
		if got != want {
			t.Fatalf("%q got %q want %q", remote, got, want)
		}
	}
}

func TestRepoKeySSHProcess(t *testing.T) {
	testTools.Setup(t)
	t.Setenv("FAKE_SSH_MODE", "success")
	if got := RepoKey(context.Background(), "git@alias:owner/repo.git"); got != "github.com/owner/repo" {
		t.Fatal(got)
	}
	t.Setenv("FAKE_SSH_MODE", "fail")
	if got := RepoKey(context.Background(), "git@alias:owner/repo.git"); got != "alias/owner/repo" {
		t.Fatal(got)
	}
	for _, mode := range []string{"slow", "tree"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("FAKE_SSH_MODE", mode)
			started, survived := filepath.Join(t.TempDir(), "started"), filepath.Join(t.TempDir(), "survived")
			t.Setenv("FAKE_SSH_STARTED", started)
			t.Setenv("FAKE_SSH_SURVIVED", survived)
			// Allow the fake SSH to boot and spawn its Match-exec child before
			// cancelling. An inherited output pipe must never delay Read until exit.
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan string, 1)
			go func() { done <- RepoKey(ctx, "git@alias:owner/repo.git") }()
			if mode == "tree" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(started); err == nil {
						break
					}
					if time.Now().After(deadline) {
						cancel()
						t.Fatal("SSH helper did not start child")
					}
					time.Sleep(10 * time.Millisecond)
				}
			} else {
				time.Sleep(100 * time.Millisecond)
			}
			cancel()
			select {
			case got := <-done:
				if got != "alias/owner/repo" {
					t.Fatal(got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SSH cancellation waited on child pipes")
			}
			if mode == "tree" {
				time.Sleep(1200 * time.Millisecond)
				if _, err := os.Stat(survived); !os.IsNotExist(err) {
					t.Fatal("SSH descendant survived cancellation", err)
				}
			}
		})
	}
}
