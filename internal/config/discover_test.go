package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit answers git commands from a table keyed by the joined arguments.
func fakeGit(answers map[string]string) func(dir string, args ...string) (string, error) {
	return func(dir string, args ...string) (string, error) {
		if out, ok := answers[strings.Join(args, " ")]; ok {
			return out, nil
		}
		return "", errors.New("exit status 1")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	cfgDir := t.TempDir()
	env := Env{
		Git: fakeGit(map[string]string{
			"rev-parse --show-toplevel":      filepath.ToSlash(root) + "\n",
			"config --get remote.origin.url": "git@github.com:me/repo.git\n",
		}),
		UserConfigDir: func() (string, error) { return cfgDir, nil },
	}
	opts, err := Discover(env, filepath.Join(root, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	want := Options{RepoRoot: root, MachinePath: filepath.Join(cfgDir, "tuckerbox", "config.toml"), Remote: "git@github.com:me/repo.git"}
	if opts != want {
		t.Errorf("got %+v, want %+v", opts, want)
	}
}

func TestDiscoverNotARepo(t *testing.T) {
	env := Env{Git: fakeGit(nil), UserConfigDir: func() (string, error) { return t.TempDir(), nil }}
	if _, err := Discover(env, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not in a git repository") {
		t.Errorf("err %v", err)
	}
}

func TestRemoteChoice(t *testing.T) {
	single := Env{Git: fakeGit(map[string]string{
		"remote":                           "upstream\n",
		"config --get remote.upstream.url": "https://example.com/a/b\n",
	})}
	if got := Remote(single, "."); got != "https://example.com/a/b" {
		t.Errorf("single remote: %q", got)
	}
	several := Env{Git: fakeGit(map[string]string{"remote": "a\nb\n"})}
	if got := Remote(several, "."); got != "" {
		t.Errorf("several remotes without origin: %q", got)
	}
	none := Env{Git: fakeGit(map[string]string{"remote": ""})}
	if got := Remote(none, "."); got != "" {
		t.Errorf("no remote: %q", got)
	}
}
