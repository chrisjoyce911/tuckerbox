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
)

// All of these run against a temporary HERDR_APP_HOME; none touches the
// person's real Herdr folder.

const keysFile = `{
  "global": {
    "cmd-t": "tab.new",
    "cmd-d": "pane.split:right"
  },
  "terminal": {
    "ctrl-1": "terminal"
  }
}
`

const mainLayout = `{ "id": "root", "type": "row", "children": [
  { "id": "main", "type": "block", "content": { "tab": "active" } } ] }
`

// herdrHome is a temp Herdr home as the app leaves it: keys.json, the
// shipped main layout (installed.json says so) shown, and a personal one.
func herdrHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	write(t, home, "keys.json", keysFile)
	write(t, home, "layouts/main.json", mainLayout)
	write(t, home, "layouts/mine.json", "{\n  \"id\": \"root\",\n  \"type\": \"column\",\n  \"floating\": [ { \"id\": \"clock\", \"content\": { \"mod\": \"clock\" }, \"edge\": \"top\" } ]\n}\n")
	write(t, home, "installed.json", `{"agents": {}, "layout:main": {}, "layout:terminals": {}}`)
	write(t, home, "shared.json", `{"layout":"main"}`)
	return home
}

func write(t *testing.T, home, rel, text string) {
	t.Helper()
	p := filepath.Join(home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, home, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// snapshot is every file under home with its bytes.
func snapshot(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(home, p)
			b, _ := os.ReadFile(p)
			out[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return out
}

// fakeHerdr is the running window for herdr-app do: layout.clone copies the
// layout file and shows the clone, layout.switch shows another. down: not running.
type fakeHerdr struct {
	home  string
	down  bool
	calls []string
}

func (f *fakeHerdr) do(_ context.Context, command string, args map[string]any) (string, error) {
	b, _ := json.Marshal(args)
	f.calls = append(f.calls, command+" "+string(b))
	if f.down {
		return "", errors.New("Herdr isn't running")
	}
	show := func(name string) {
		b, _ := json.Marshal(map[string]any{"layout": name})
		os.WriteFile(filepath.Join(f.home, "shared.json"), b, 0o644)
	}
	switch command {
	case "layout.clone":
		src, _ := os.ReadFile(layoutPath(f.home, args["name"].(string)))
		os.WriteFile(layoutPath(f.home, args["as"].(string)), src, 0o644)
		show(args["as"].(string))
	case "layout.switch":
		show(args["name"].(string))
	}
	return "{}", nil
}

func extrasEnv(t *testing.T, home, goos string) (Env, *fakeHerdr) {
	var dirs []string
	e := env(t, home, passCheck(&dirs))
	f := &fakeHerdr{home: home}
	e.Do, e.GOOS = f.do, goos
	return e, f
}

func readRec(t *testing.T, home string) *Record {
	t.Helper()
	r, err := readRecord(home)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestToggleKeyFromTheModsKeysFile(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "cmd-shift-k", "linux": "ctrl-shift-k", "windows": "ctrl-shift-k"} {
		e, _ := extrasEnv(t, t.TempDir(), goos)
		key, action, err := ToggleBinding(e)
		if err != nil || key != want || action != "mod.open:tuckerbox-board" {
			t.Errorf("%s: %q %q %v", goos, key, action, err)
		}
	}
}

func TestKeysAddTheToggleKeepingTheFile(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	did, err := InstallExtras(context.Background(), e, Options{Keys: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(did) != 1 || !strings.Contains(did[0], "cmd-shift-k now opens and hides the board") {
		t.Errorf("did %q", did)
	}
	got := read(t, home, "keys.json")
	want := `{
  "global": {
    "cmd-t": "tab.new",
    "cmd-d": "pane.split:right",
    "cmd-shift-k": "mod.open:tuckerbox-board"
  },
  "terminal": {
    "ctrl-1": "terminal"
  }
}
`
	if got != want {
		t.Errorf("keys.json:\n%s", got)
	}
	r := readRec(t, home)
	if r.Keys == nil || r.Keys.Before != keysFile || r.Keys.Key != "cmd-shift-k" || r.Keys.Section != "global" {
		t.Errorf("record %+v", r.Keys)
	}
	// Again: nothing changes.
	did, err = InstallExtras(context.Background(), e, Options{Keys: true})
	if err != nil || !strings.Contains(did[0], "already") || read(t, home, "keys.json") != want {
		t.Errorf("second run: %q %v", did, err)
	}
}

func TestKeyClashChangesNothing(t *testing.T) {
	for _, tc := range []struct{ file, section string }{
		{`{"global": {"shift-cmd-k": "app.palette"}}`, "global"},
		{`{"global": {}, "terminal": {"cmd-shift-K": "terminal"}}`, "terminal"},
		{`{"layout:terminals": {"cmd-shift-k": {"command": "layout.resize", "args": {}}}}`, "layout:terminals"},
	} {
		home := herdrHome(t)
		write(t, home, "keys.json", tc.file)
		e, _ := extrasEnv(t, home, "darwin")
		_, err := InstallExtras(context.Background(), e, Options{Keys: true})
		var clash *KeyClash
		if !errors.As(err, &clash) || clash.Section != tc.section {
			t.Errorf("%s: err %v", tc.file, err)
			continue
		}
		if !strings.Contains(err.Error(), "already bound") || !strings.Contains(err.Error(), "--key") {
			t.Errorf("message %q", err)
		}
		if read(t, home, "keys.json") != tc.file {
			t.Errorf("%s: keys.json changed", tc.file)
		}
		if r := readRec(t, home); r != nil && r.Keys != nil {
			t.Errorf("recorded a key it didn't add: %+v", r.Keys)
		}
	}
	// A key in a mod: section only works inside that mod: not a clash.
	home := herdrHome(t)
	write(t, home, "keys.json", `{"mod:editor": {"cmd-shift-k": "editor.kill"}}`)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true}); err != nil {
		t.Errorf("mod section: %v", err)
	}
}

func TestKeyChosenAndKeyAlreadyThere(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "linux")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true, Key: "f8"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, home, "keys.json"), `"f8": "mod.open:tuckerbox-board"`) {
		t.Error("f8 not bound")
	}
	// Bound by the person before install: kept, and not recorded as install's.
	home2 := herdrHome(t)
	mine := `{"global": {"ctrl-shift-k": "mod.open:tuckerbox-board"}}`
	write(t, home2, "keys.json", mine)
	e2, _ := extrasEnv(t, home2, "linux")
	did, err := InstallExtras(context.Background(), e2, Options{Keys: true})
	if err != nil || !strings.Contains(did[0], "bound before install") || read(t, home2, "keys.json") != mine {
		t.Errorf("%q %v", did, err)
	}
	if r := readRec(t, home2); r != nil && r.Keys != nil {
		t.Errorf("recorded %+v", r.Keys)
	}
}

func TestNoKeysFileWritesNone(t *testing.T) {
	home := herdrHome(t)
	os.Remove(filepath.Join(home, "keys.json"))
	e, _ := extrasEnv(t, home, "linux")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true}); !errors.Is(err, ErrNoKeysFile) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "keys.json")); err == nil {
		t.Error("wrote keys.json")
	}
}

func drawerOf(t *testing.T, text string) map[string]any {
	t.Helper()
	var l struct {
		Floating []map[string]any `json:"floating"`
	}
	if err := json.Unmarshal([]byte(text), &l); err != nil {
		t.Fatal(err)
	}
	for _, f := range l.Floating {
		if f["id"] == DrawerID {
			return f
		}
	}
	return nil
}

func TestDrawerInAPersonalLayout(t *testing.T) {
	home := herdrHome(t)
	write(t, home, "shared.json", `{"layout": "mine"}`)
	before := read(t, home, "layouts/mine.json")
	e, f := extrasEnv(t, home, "linux")
	did, err := InstallExtras(context.Background(), e, Options{Drawer: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Errorf("a personal layout needs no clone: %v", f.calls)
	}
	if !strings.Contains(did[0], "your layout mine") {
		t.Errorf("did %q", did)
	}
	after := read(t, home, "layouts/mine.json")
	d := drawerOf(t, after)
	if d == nil || d["edge"] != "right" || d["peek"] != float64(48) {
		t.Fatalf("drawer %v in\n%s", d, after)
	}
	if c := d["content"].(map[string]any); c["mod"] != "tuckerbox-board" || c["props"].(map[string]any)["view"] != "drawer" {
		t.Errorf("content %v", c)
	}
	if !strings.Contains(after, `"id": "clock"`) || strings.Index(after, `"clock"`) > strings.Index(after, DrawerID) {
		t.Error("the person's floating block is kept, first")
	}
	if _, err := Uninstall(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if read(t, home, "layouts/mine.json") != before {
		t.Error("uninstall didn't put the layout back byte for byte")
	}
}

func TestDrawerInAShippedLayoutClonesIt(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	did, err := InstallExtras(context.Background(), e, Options{Drawer: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != `layout.clone {"as":"main-tuckerbox","name":"main"}` {
		t.Errorf("calls %v", f.calls)
	}
	if read(t, home, "layouts/main.json") != mainLayout {
		t.Error("the shipped layout was edited")
	}
	if drawerOf(t, read(t, home, "layouts/main-tuckerbox.json")) == nil {
		t.Error("no drawer in the clone")
	}
	if !strings.Contains(did[0], "a copy of Herdr's main") {
		t.Errorf("did %q", did)
	}
	r := readRec(t, home)
	if r.Drawer == nil || r.Drawer.Layout != "main-tuckerbox" || r.Drawer.ClonedFrom != "main" {
		t.Errorf("record %+v", r.Drawer)
	}
	// Again: nothing new.
	if _, err := InstallExtras(context.Background(), e, Options{Drawer: true}); err != nil || len(f.calls) != 1 {
		t.Errorf("second run cloned again: %v %v", f.calls, err)
	}
	// Uninstall: back to main, the clone gone.
	did, err = Uninstall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != `layout.switch {"name":"main"}` {
		t.Errorf("calls %v", f.calls)
	}
	if _, err := os.Stat(layoutPath(home, "main-tuckerbox")); err == nil {
		t.Error("clone kept")
	}
	if !strings.Contains(strings.Join(did, "\n"), "switched back to main") {
		t.Errorf("did %q", did)
	}
}

func TestDrawerCloneNeedsHerdrRunning(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	f.down = true
	before := snapshot(t, home)
	_, err := InstallExtras(context.Background(), e, Options{Drawer: true})
	if !errors.Is(err, ErrHerdrNotRunning) || !strings.Contains(err.Error(), "open Herdr") {
		t.Fatalf("err %v", err)
	}
	got := snapshot(t, home)
	if len(got) != len(before) {
		t.Errorf("wrote files: %v", got)
	}
	for k, v := range before {
		if got[k] != v {
			t.Errorf("%s changed", k)
		}
	}
}

func TestUninstallReversesEverything(t *testing.T) {
	home := herdrHome(t)
	before := snapshot(t, home)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true, Drawer: true}); err != nil {
		t.Fatal(err)
	}
	if r := readRec(t, home); len(r.Mods) != 1 || r.Mods[0] != "tuckerbox-board" || r.Keys == nil || r.Drawer == nil {
		t.Fatalf("record %+v", r)
	}
	did, err := Uninstall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, home)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s not back as it was:\n%s", k, after[k])
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("left %s", k)
		}
	}
	if len(did) != 3 {
		t.Errorf("did %q", did)
	}
	if _, err := Uninstall(context.Background(), e); !errors.Is(err, ErrNothingInstalled) {
		t.Errorf("second uninstall: %v", err)
	}
}

func TestUninstallKeepsWhatThePersonChangedSince(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true, Drawer: true}); err != nil {
		t.Fatal(err)
	}
	// The person adds a key and edits the clone.
	k := strings.Replace(read(t, home, "keys.json"), `"cmd-t": "tab.new",`, `"cmd-t": "tab.new", "cmd-y": "app.help",`, 1)
	write(t, home, "keys.json", k)
	l := strings.Replace(read(t, home, "layouts/main-tuckerbox.json"), `"type": "row"`, `"type": "row", "theme": "moss"`, 1)
	write(t, home, "layouts/main-tuckerbox.json", l)
	did, err := Uninstall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	keys := read(t, home, "keys.json")
	if strings.Contains(keys, "cmd-shift-k") || !strings.Contains(keys, `"cmd-y": "app.help"`) {
		t.Errorf("keys.json:\n%s", keys)
	}
	clone := read(t, home, "layouts/main-tuckerbox.json")
	if drawerOf(t, clone) != nil || !strings.Contains(clone, `"theme": "moss"`) {
		t.Errorf("clone:\n%s", clone)
	}
	if strings.Contains(clone, "floating") {
		t.Error("an empty floating list was left")
	}
	if !strings.Contains(strings.Join(did, "\n"), "kept the layout") {
		t.Errorf("did %q", did)
	}
}

func TestUninstallWithoutARecord(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "linux")
	if _, err := Uninstall(context.Background(), e); !errors.Is(err, ErrNothingInstalled) {
		t.Fatalf("err %v", err)
	}
	// An install from before the record: the mod folder only.
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	os.Remove(recordPath(home))
	did, err := Uninstall(context.Background(), e)
	if err != nil || len(did) != 1 || !strings.Contains(did[0], "no install record") {
		t.Fatalf("%q %v", did, err)
	}
	if _, err := os.Stat(filepath.Join(home, "mods", "tuckerbox-board")); err == nil {
		t.Error("mod kept")
	}
	if _, err := Uninstall(context.Background(), env(t, filepath.Join(home, "nope"), nil)); !errors.Is(err, ErrNoHerdr) {
		t.Errorf("no Herdr: %v", err)
	}
}

func TestOrderedJSONKeepsOrder(t *testing.T) {
	in := `{"z": 1, "a": [true, null, 2.50, {"y": "<b>", "b": {}}], "m": []}`
	v, err := parseOrdered([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"z\": 1,\n  \"a\": [\n    true,\n    null,\n    2.50,\n    {\n      \"y\": \"<b>\",\n      \"b\": {}\n    }\n  ],\n  \"m\": []\n}\n"
	if got := string(encodeOrdered(v)); got != want {
		t.Errorf("got\n%s", got)
	}
	if _, err := parseOrdered([]byte(`{} {}`)); err == nil {
		t.Error("two values accepted")
	}
	for k, want := range map[string]string{"shift-cmd-K": "cmd-shift-k", "cmd--": "cmd--", "f8": "f8"} {
		if got := normKey(k); got != normKey(want) {
			t.Errorf("normKey(%q) = %q", k, got)
		}
	}
}
