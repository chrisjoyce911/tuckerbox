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
		Getenv: func(k string) string {
			if k == "XDG_CONFIG_HOME" {
				return cfgDir
			}
			return ""
		},
	}
	opts, err := Discover(env, filepath.Join(root, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	want := Options{RepoRoot: root, MachinePath: filepath.Join(cfgDir, "tuckerbox", "config.toml"), Remote: "git@github.com:me/repo.git"}
	if opts.RepoRoot != want.RepoRoot || opts.MachinePath != want.MachinePath || opts.Remote != want.Remote || opts.PushURL == nil {
		t.Errorf("got %+v, want %+v", opts, want)
	}
}

func TestDiscoverNotARepo(t *testing.T) {
	env := Env{Git: fakeGit(nil), Getenv: func(string) string { return "" }}
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

func TestMachinePath(t *testing.T) {
	xdg := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	appData := filepath.Join(t.TempDir(), "AppData", "Roaming")
	env := func(goos, xdgValue string) Env {
		return Env{
			GOOS: goos,
			Getenv: func(k string) string {
				if k == "XDG_CONFIG_HOME" {
					return xdgValue
				}
				return ""
			},
			HomeDir:       func() (string, error) { return home, nil },
			UserConfigDir: func() (string, error) { return appData, nil },
		}
	}
	for _, c := range []struct {
		name, goos, xdg, want string
	}{
		{"xdg on linux", "linux", xdg, filepath.Join(xdg, "tuckerbox", "config.toml")},
		{"xdg on darwin", "darwin", xdg, filepath.Join(xdg, "tuckerbox", "config.toml")},
		{"xdg on windows", "windows", xdg, filepath.Join(xdg, "tuckerbox", "config.toml")},
		{"linux", "linux", "", filepath.Join(home, ".config", "tuckerbox", "config.toml")},
		{"darwin", "darwin", "", filepath.Join(home, ".config", "tuckerbox", "config.toml")},
		{"relative xdg ignored", "darwin", "rel/dir", filepath.Join(home, ".config", "tuckerbox", "config.toml")},
		{"windows", "windows", "", filepath.Join(appData, "tuckerbox", "config.toml")},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := MachinePath(env(c.goos, c.xdg))
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %s, want %s", got, c.want)
			}
		})
	}
	failing := env("linux", "")
	failing.HomeDir = func() (string, error) { return "", errors.New("no home") }
	if _, err := MachinePath(failing); err == nil {
		t.Error("no error without a home directory")
	}
}
