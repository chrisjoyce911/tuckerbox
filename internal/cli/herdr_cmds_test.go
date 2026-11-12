package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/herdrmod"
)

// herdrApp is app() with Herdr's home at home (a temp folder, never the
// real one) and a herdr-app check that passes.
func herdrApp(t *testing.T, home string, installed ...string) (App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	a, out, errOut := app(installed...)
	a.HerdrEnv = herdrmod.Env{
		Getenv: func(k string) string {
			if k == "HERDR_APP_HOME" {
				return home
			}
			return ""
		},
		UserHome: func() (string, error) { return "", errors.New("tests use HERDR_APP_HOME") },
		Check:    func(context.Context, string) (string, error) { return "check passed\n", nil },
		Do: func(context.Context, string, map[string]any) (string, error) {
			return "", errors.New("Herdr isn't running")
		},
		GOOS: "linux",
	}
	return a, out, errOut
}

func TestHerdrInstallWithoutHerdr(t *testing.T) {
	parent := t.TempDir()
	a, out, errOut := herdrApp(t, filepath.Join(parent, "herdr-app"))
	if code := a.Run([]string{"herdr", "install"}); code == 0 {
		t.Fatalf("exit 0; out %s", out)
	}
	if !strings.Contains(errOut.String(), "Herdr wasn't found") || !strings.Contains(errOut.String(), "Nothing was written") {
		t.Errorf("stderr %q", errOut)
	}
	if es, _ := os.ReadDir(parent); len(es) != 0 {
		t.Errorf("wrote %v", es)
	}
}

func TestHerdrInstallThenUpdate(t *testing.T) {
	home := t.TempDir()
	a, out, errOut := herdrApp(t, home)
	if code := a.Run([]string{"herdr", "install"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	dir := filepath.Join(home, "mods", "tuckerbox-board")
	if !strings.Contains(out.String(), "installed the board mod") || !strings.Contains(out.String(), dir) || !strings.Contains(out.String(), "herdr-app check passed") {
		t.Errorf("out %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "main.js")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := a.Run([]string{"herdr", "install"}); code != 0 || !strings.Contains(out.String(), "updated the board mod") {
		t.Errorf("exit %d out %q", code, out)
	}
}

func TestHerdrInstallRejectedByCheck(t *testing.T) {
	home := t.TempDir()
	a, _, errOut := herdrApp(t, home)
	a.HerdrEnv.Check = func(context.Context, string) (string, error) { return "TypeError: x\n", errors.New("exit 1") }
	if code := a.Run([]string{"herdr", "install"}); code != 1 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), "TypeError") || !strings.Contains(errOut.String(), "nothing was installed") {
		t.Errorf("stderr %q", errOut)
	}
	if es, _ := os.ReadDir(home); len(es) != 0 {
		t.Errorf("wrote %v", es)
	}
}

func TestHerdrUsage(t *testing.T) {
	for _, args := range [][]string{{"herdr"}, {"herdr", "install", "--menu"}, {"herdr", "install", "--key", "f8"}, {"herdr", "uninstall", "--keys"}, {"herdr", "remove"}} {
		a, _, errOut := herdrApp(t, t.TempDir())
		if code := a.Run(args); code != 2 || !strings.Contains(errOut.String(), "tuckerbox herdr: ") {
			t.Errorf("%v: exit %d %q", args, code, errOut)
		}
	}
}

// A Herdr home as the app leaves it: keys.json and a personal layout shown.
func herdrHomeWithLayout(t *testing.T, keys string) string {
	t.Helper()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "layouts"), 0o755)
	os.WriteFile(filepath.Join(home, "keys.json"), []byte(keys), 0o644)
	os.WriteFile(filepath.Join(home, "layouts", "mine.json"), []byte(`{"id": "root", "type": "row"}`), 0o644)
	os.WriteFile(filepath.Join(home, "shared.json"), []byte(`{"layout":"mine"}`), 0o644)
	return home
}

func TestHerdrInstallKeysDrawerThenUninstall(t *testing.T) {
	home := herdrHomeWithLayout(t, `{"global": {"ctrl-shift-t": "tab.new"}}`)
	a, out, errOut := herdrApp(t, home)
	if code := a.Run([]string{"herdr", "install", "--keys", "--drawer"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out.String(), "ctrl-shift-k now opens and hides the board") || !strings.Contains(out.String(), "added the drawer to your layout mine") {
		t.Errorf("out %q", out)
	}
	keys, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	if !strings.Contains(string(keys), `"ctrl-shift-k": "mod.open:tuckerbox-board"`) {
		t.Errorf("keys.json %s", keys)
	}
	out.Reset()
	if code := a.Run([]string{"herdr", "uninstall"}); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	keys, _ = os.ReadFile(filepath.Join(home, "keys.json"))
	layout, _ := os.ReadFile(filepath.Join(home, "layouts", "mine.json"))
	if string(keys) != `{"global": {"ctrl-shift-t": "tab.new"}}` || string(layout) != `{"id": "root", "type": "row"}` {
		t.Errorf("not put back: %s %s", keys, layout)
	}
	if _, err := os.Stat(filepath.Join(home, "mods", "tuckerbox-board")); err == nil {
		t.Error("mod kept")
	}
	if !strings.Contains(out.String(), "uninstalled") {
		t.Errorf("out %q", out)
	}
	if code := a.Run([]string{"herdr", "uninstall"}); code != 1 || !strings.Contains(errOut.String(), "isn't installed") {
		t.Errorf("second uninstall: exit %d %q", code, errOut)
	}
}

func TestHerdrInstallKeyClashIsReported(t *testing.T) {
	home := herdrHomeWithLayout(t, `{"global": {"ctrl-shift-k": "app.palette"}}`)
	a, out, errOut := herdrApp(t, home)
	if code := a.Run([]string{"herdr", "install", "--keys", "--drawer"}); code != 1 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), "ctrl-shift-k is already bound to app.palette") {
		t.Errorf("stderr %q", errOut)
	}
	// The mod and the drawer are still installed; the key is left alone.
	if !strings.Contains(out.String(), "installed the board mod") || !strings.Contains(out.String(), "added the drawer") {
		t.Errorf("out %q", out)
	}
	keys, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	if string(keys) != `{"global": {"ctrl-shift-k": "app.palette"}}` {
		t.Errorf("keys.json changed: %s", keys)
	}
}

func TestDoctorReportsTheBoardMod(t *testing.T) {
	home := t.TempDir()
	tools := []string{"git", "openspec"}
	// Herdr without the mod.
	a, out, _ := herdrApp(t, home, tools...)
	if code := a.Run([]string{"doctor"}); code != 0 || !strings.Contains(out.String(), "herdr board mod: not installed (tuckerbox herdr install)") {
		t.Errorf("exit %d out %q", code, out)
	}
	// Installed by this binary: current.
	a.Run([]string{"herdr", "install"})
	out.Reset()
	a.Run([]string{"doctor"})
	if !strings.Contains(out.String(), "herdr board mod: ") || !strings.Contains(out.String(), ", current") {
		t.Errorf("out %q", out)
	}
	// A shipped file changed by hand, another deleted: named, doctor passes.
	dir := filepath.Join(home, "mods", "tuckerbox-board")
	os.WriteFile(filepath.Join(dir, "main.js"), []byte("broken"), 0o644)
	os.Remove(filepath.Join(dir, "ui.js"))
	out.Reset()
	if code := a.Run([]string{"doctor"}); code != 0 {
		t.Errorf("doctor failed on a changed mod: %d", code)
	}
	if !strings.Contains(out.String(), "differs from what this tuckerbox ships (main.js changed, ui.js missing): run tuckerbox herdr install") {
		t.Errorf("out %q", out)
	}
	a.Run([]string{"herdr", "install"})
	// An older copy: a warning, and doctor still passes.
	vf := filepath.Join(home, "mods", "tuckerbox-board", herdrmod.VersionFile)
	os.WriteFile(vf, []byte(`{"version":"v0.0.1","commit":"old","digest":"sha256:old"}`), 0o644)
	out.Reset()
	if code := a.Run([]string{"doctor"}); code != 0 {
		t.Errorf("doctor failed on an old mod: %d", code)
	}
	if !strings.Contains(out.String(), "warning: herdr board mod (v0.0.1) is not the one this tuckerbox") || !strings.Contains(out.String(), "tuckerbox herdr install") {
		t.Errorf("out %q", out)
	}
	// No Herdr: no line.
	b, out2, _ := herdrApp(t, filepath.Join(home, "absent"), tools...)
	b.Run([]string{"doctor"})
	if strings.Contains(out2.String(), "herdr") {
		t.Errorf("out %q", out2)
	}
}
