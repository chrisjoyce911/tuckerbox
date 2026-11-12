package herdrmod

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// env points at a temporary Herdr home (never the real one) and a fake check.
func env(t *testing.T, home string, check func(ctx context.Context, dir string) (string, error)) Env {
	t.Helper()
	return Env{
		Getenv: func(k string) string {
			if k == "HERDR_APP_HOME" {
				return home
			}
			return ""
		},
		UserHome: func() (string, error) { return "", errors.New("tests use HERDR_APP_HOME") },
		Check:    check,
	}
}

func passCheck(dirs *[]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, dir string) (string, error) {
		*dirs = append(*dirs, dir)
		if _, err := os.Stat(filepath.Join(dir, "main.js")); err != nil {
			return "no main.js", errors.New("exit 1")
		}
		return "check passed: " + dir + "\n", nil
	}
}

func list(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func TestEmbeddedModHasItsFilesOnly(t *testing.T) {
	mod, err := Env{}.mod()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.js", "mod.json", "board.js", "card.js", "actions.js", "model.js", "spinner.svg", "fixtures/board.json", "fixtures/detail.json"} {
		if _, err := fs.Stat(mod, want); err != nil {
			t.Errorf("embedded mod lacks %s", want)
		}
	}
	for _, no := range []string{"package.json", "dev.json", "jsconfig.json", "gpui-kit.d.ts", "test/actions.test.js"} {
		if _, err := fs.Stat(mod, no); err == nil {
			t.Errorf("embedded mod has %s", no)
		}
	}
}

func TestHerdrMissingWritesNothing(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "herdr-app")
	var dirs []string
	_, err := Install(context.Background(), env(t, home, passCheck(&dirs)), "v1", "abc")
	if !errors.Is(err, ErrNoHerdr) {
		t.Fatalf("err = %v, want ErrNoHerdr", err)
	}
	if !strings.Contains(err.Error(), "Herdr wasn't found") {
		t.Errorf("message %q", err)
	}
	if got := list(t, parent); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
	if _, err := os.Stat(home); err == nil {
		t.Error("made the Herdr home")
	}
	if len(dirs) != 0 {
		t.Error("ran the check")
	}
}

func TestInstallChecksAndCopies(t *testing.T) {
	home := t.TempDir()
	var dirs []string
	e := env(t, home, passCheck(&dirs))
	res, err := Install(context.Background(), e, "v1.2.3", "abc1234")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Checked || res.Updated {
		t.Errorf("result %+v", res)
	}
	want := filepath.Join(home, "mods", "tuckerbox-board")
	if res.Dir != want {
		t.Errorf("dir %s, want %s", res.Dir, want)
	}
	if len(dirs) != 1 || strings.HasPrefix(dirs[0], want) {
		t.Errorf("check ran on %v; want one staged copy, not the installed folder", dirs)
	}
	got := list(t, res.Dir)
	mod, _ := Env{}.mod()
	n := 0
	fs.WalkDir(mod, ".", func(_ string, d fs.DirEntry, _ error) error {
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if len(got) != n+1 {
		t.Errorf("installed %d files, want %d and version.json: %v", len(got), n, got)
	}
	var st Stamp
	b, _ := os.ReadFile(filepath.Join(res.Dir, VersionFile))
	if err := json.Unmarshal(b, &st); err != nil || st.Version != "v1.2.3" || st.Commit != "abc1234" || !strings.HasPrefix(st.Digest, "sha256:") {
		t.Errorf("version.json %s (%v)", b, err)
	}
	// Nothing staged is left beside mods/.
	for _, e := range mustRead(t, home) {
		if strings.HasPrefix(e, ".tuckerbox-board") {
			t.Errorf("left %s", e)
		}
	}
	s, err := Inspect(e)
	if err != nil || !s.Herdr || !s.Installed || !s.Current || s.Stamp.Version != "v1.2.3" {
		t.Errorf("inspect %+v %v", s, err)
	}
}

func mustRead(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestReinstallUpdatesInPlace(t *testing.T) {
	home := t.TempDir()
	var dirs []string
	e := env(t, home, passCheck(&dirs))
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "mods", "tuckerbox-board")
	// A stale file from an older copy, and the user's other mods.
	os.WriteFile(filepath.Join(dir, "old.js"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(home, "mods", "board"), 0o755)
	os.WriteFile(filepath.Join(home, "mods", "board", "main.js"), []byte("kelpie"), 0o644)
	res, err := Install(context.Background(), e, "v2", "b")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated {
		t.Error("not marked updated")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.js")); err == nil {
		t.Error("the old copy's extra file survived")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "mods", "board", "main.js")); string(b) != "kelpie" {
		t.Error("touched another mod")
	}
	s, _ := Inspect(e)
	if s.Stamp.Version != "v2" || !s.Current {
		t.Errorf("inspect %+v", s)
	}
}

func TestCheckFailureInstallsNothing(t *testing.T) {
	home := t.TempDir()
	e := env(t, home, func(context.Context, string) (string, error) {
		return "SyntaxError at main.js:3\n", errors.New("exit 1")
	})
	_, err := Install(context.Background(), e, "v1", "a")
	var ce *CheckError
	if !errors.As(err, &ce) || !strings.Contains(ce.Output, "SyntaxError") {
		t.Fatalf("err = %v", err)
	}
	if got := mustRead(t, home); len(got) != 0 {
		t.Errorf("left %v", got)
	}
}

func TestCheckFailureKeepsTheEarlierCopy(t *testing.T) {
	home := t.TempDir()
	var dirs []string
	if _, err := Install(context.Background(), env(t, home, passCheck(&dirs)), "v1", "a"); err != nil {
		t.Fatal(err)
	}
	bad := env(t, home, func(context.Context, string) (string, error) { return "broken", errors.New("exit 1") })
	if _, err := Install(context.Background(), bad, "v2", "b"); err == nil {
		t.Fatal("no error")
	}
	s, _ := Inspect(bad)
	if s.Stamp.Version != "v1" {
		t.Errorf("earlier copy replaced: %+v", s)
	}
}

func TestNoHerdrAppStillInstalls(t *testing.T) {
	home := t.TempDir()
	e := env(t, home, func(context.Context, string) (string, error) { return "", ErrNoChecker })
	res, err := Install(context.Background(), e, "v1", "a")
	if err != nil || res.Checked {
		t.Fatalf("res %+v err %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(res.Dir, "main.js")); err != nil {
		t.Error("not installed")
	}
}

func TestInspectOutdatedAndMissing(t *testing.T) {
	home := t.TempDir()
	e := env(t, home, nil)
	if s, _ := Inspect(e); !s.Herdr || s.Installed {
		t.Errorf("empty home: %+v", s)
	}
	none := env(t, filepath.Join(home, "absent"), nil)
	if s, _ := Inspect(none); s.Herdr {
		t.Errorf("absent home: %+v", s)
	}
	dir := filepath.Join(home, "mods", "tuckerbox-board")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, VersionFile), []byte(`{"version":"v0.1","digest":"sha256:old"}`), 0o644)
	s, _ := Inspect(e)
	if !s.Installed || s.Current || s.Stamp.Version != "v0.1" {
		t.Errorf("old copy: %+v", s)
	}
}

func TestDigestCoversPathsAndContent(t *testing.T) {
	a := fstest.MapFS{"a.js": {Data: []byte("x")}, "b.js": {Data: []byte("")}}
	b := fstest.MapFS{"a.js": {Data: []byte("")}, "b.js": {Data: []byte("x")}}
	c := fstest.MapFS{"a.js": {Data: []byte("x")}, "b.js": {Data: []byte("")}, VersionFile: {Data: []byte("{}")}}
	da, _ := Digest(a)
	db, _ := Digest(b)
	dc, _ := Digest(c)
	if da == db {
		t.Error("moving bytes between files kept the digest")
	}
	if da != dc {
		t.Error("version.json changed the digest")
	}
}

func TestInspectSeesFilesChangedByHand(t *testing.T) {
	home := t.TempDir()
	var dirs []string
	e := env(t, home, passCheck(&dirs))
	res, err := Install(context.Background(), e, "v1", "a")
	if err != nil {
		t.Fatal(err)
	}
	// Files Herdr or a person adds don't count.
	for _, n := range []string{"gpui-kit.d.ts", "jsconfig.json", "dev.json"} {
		os.WriteFile(filepath.Join(res.Dir, n), []byte("{}"), 0o644)
	}
	if s, _ := Inspect(e); !s.Current || len(s.Modified)+len(s.Missing) != 0 {
		t.Fatalf("added files counted: %+v", s)
	}
	// Modify one shipped file, delete another; the stamp stays as installed.
	os.WriteFile(filepath.Join(res.Dir, "main.js"), []byte("broken"), 0o644)
	os.Remove(filepath.Join(res.Dir, "ui.js"))
	os.Remove(filepath.Join(res.Dir, "fixtures", "detail.json"))
	s, err := Inspect(e)
	if err != nil {
		t.Fatal(err)
	}
	if s.Current {
		t.Error("changed copy reported current")
	}
	if strings.Join(s.Modified, ",") != "main.js" {
		t.Errorf("modified %v", s.Modified)
	}
	if strings.Join(s.Missing, ",") != "fixtures/detail.json,ui.js" {
		t.Errorf("missing %v", s.Missing)
	}
	// Installing again puts it right.
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	if s, _ := Inspect(e); !s.Current {
		t.Errorf("after reinstall %+v", s)
	}
}
