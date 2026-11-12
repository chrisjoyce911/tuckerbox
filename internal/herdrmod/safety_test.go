package herdrmod

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests for openspec/changes/fix-board-safety/specs/board-install.

// failWrites makes replaceFile fail for the writes fail picks, until the
// returned function (or the test's end) clears it.
func failWrites(t *testing.T, fail func(path string, b []byte) bool) (clear func()) {
	t.Helper()
	orig := replaceFile
	replaceFile = func(path string, b []byte) error {
		if fail(path, b) {
			return errors.New("injected write fault")
		}
		return orig(path, b)
	}
	clear = func() { replaceFile = orig }
	t.Cleanup(clear)
	return clear
}

func failRemoves(t *testing.T, fail func(path string) bool) {
	t.Helper()
	orig := removeFile
	removeFile = func(path string) error {
		if fail(path) {
			return errors.New("injected remove fault")
		}
		return orig(path)
	}
	t.Cleanup(func() { removeFile = orig })
}

func exists(home, rel string) bool {
	_, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel)))
	return err == nil
}

func sameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s was written", k)
		}
	}
}

// Scenario: Keys and drawer installed at once.
func TestKeysAndDrawerInstalledAtOnce(t *testing.T) {
	home := herdrHome(t)
	before := read(t, home, "keys.json")
	keysEnv, _ := extrasEnv(t, home, "darwin")
	drawerEnv, _ := extrasEnv(t, home, "darwin")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, run := range []struct {
		e   Env
		opt Options
	}{{keysEnv, Options{Keys: true}}, {drawerEnv, Options{Drawer: true}}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Install(context.Background(), run.e, "v1", "a"); err != nil {
				errs[i] = err
				return
			}
			_, errs[i] = InstallExtras(context.Background(), run.e, run.opt)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	r := readRec(t, home)
	if r.Keys == nil || r.Drawer == nil || len(r.Mods) != 1 {
		t.Fatalf("record lost a change: %+v", r)
	}
	if _, err := Uninstall(context.Background(), drawerEnv); err != nil {
		t.Fatal(err)
	}
	if read(t, home, "keys.json") != before || exists(home, "layouts/main-tuckerbox.json") {
		t.Error("uninstall didn't remove both")
	}
}

// Scenario: Lock held too long.
func TestLockHeldTooLong(t *testing.T) {
	defer func(w time.Duration) { lockWait = w }(lockWait)
	lockWait = 300 * time.Millisecond
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	unlock, err := lockHome(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	before := snapshot(t, home)
	_, err = InstallExtras(context.Background(), e, Options{Keys: true, Drawer: true})
	if !errors.Is(err, ErrBusy) || !strings.Contains(err.Error(), "another tuckerbox herdr install or uninstall is running") {
		t.Errorf("install: %v", err)
	}
	if _, err := Install(context.Background(), e, "v1", "a"); !errors.Is(err, ErrBusy) {
		t.Errorf("mod install: %v", err)
	}
	if _, err := Uninstall(context.Background(), e); !errors.Is(err, ErrBusy) {
		t.Errorf("uninstall: %v", err)
	}
	sameFiles(t, before, snapshot(t, home))
}

// Scenario: Journal can't be written.
func TestJournalCantBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder this user can't write to")
	}
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, home)
	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(home, 0o755)
	_, err := InstallExtras(context.Background(), e, Options{Keys: true})
	if err == nil || !strings.Contains(err.Error(), JournalFile) {
		t.Fatalf("err %v", err)
	}
	os.Chmod(home, 0o755)
	sameFiles(t, before, snapshot(t, home))
}

// Scenario: Record write fails after the key.
func TestRecordWriteFailsAfterTheKey(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	rec := read(t, home, RecordFile)
	failWrites(t, func(p string, _ []byte) bool { return p == recordPath(home) })
	_, err := InstallExtras(context.Background(), e, Options{Keys: true})
	if err == nil || !strings.Contains(err.Error(), RecordFile) {
		t.Fatalf("err %v", err)
	}
	if read(t, home, "keys.json") != keysFile {
		t.Error("keys.json not put back")
	}
	if read(t, home, RecordFile) != rec {
		t.Error("record changed")
	}
	if exists(home, JournalFile) {
		t.Error("journal left")
	}
}

// leaveJournal runs install --keys with the record write and the restore of
// keys.json both failing, as in "Record write and rollback both fail". The
// returned function clears the faults.
func leaveJournal(t *testing.T, home string, e Env) (clear func()) {
	t.Helper()
	clear = failWrites(t, func(p string, b []byte) bool {
		return p == recordPath(home) || (filepath.Base(p) == "keys.json" && string(b) == keysFile)
	})
	did, err := InstallExtras(context.Background(), e, Options{Keys: true})
	if err == nil {
		t.Fatal("install succeeded")
	}
	if !strings.Contains(strings.Join(did, "\n"), "run tuckerbox herdr uninstall") {
		t.Errorf("output doesn't say to run uninstall: %q", did)
	}
	return clear
}

// Scenario: Record write and rollback both fail.
func TestRecordWriteAndRollbackBothFail(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	leaveJournal(t, home, e)
	var j struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(read(t, home, JournalFile)), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Entries) != 1 || j.Entries[0]["file"] != "keys.json" || j.Entries[0]["before"] != keysFile {
		t.Errorf("journal %v", j.Entries)
	}
}

// Scenario: Uninstall finishes the undo.
func TestUninstallFinishesTheUndo(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	leaveJournal(t, home, e)()
	if _, err := Uninstall(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if read(t, home, "keys.json") != keysFile {
		t.Error("keys.json not as before")
	}
	if exists(home, JournalFile) || exists(home, RecordFile) {
		t.Error("journal or record left")
	}
}

// Scenario: File edited since.
func TestJournalFileEditedSince(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	leaveJournal(t, home, e)()
	edited := strings.Replace(read(t, home, "keys.json"), `"cmd-t": "tab.new",`, `"cmd-t": "tab.new", "cmd-y": "app.help",`, 1)
	write(t, home, "keys.json", edited)
	did, err := InstallExtras(context.Background(), e, Options{Drawer: true})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, home, "keys.json") != edited {
		t.Error("keys.json overwritten")
	}
	if !strings.Contains(strings.Join(did, "\n"), "keys.json changed since that install") {
		t.Errorf("not reported: %q", did)
	}
	if exists(home, JournalFile) {
		t.Error("journal left")
	}
	if r := readRec(t, home); r == nil || r.Drawer == nil {
		t.Error("install didn't carry on")
	}
}

// Scenarios: Fault still there on the next try, then Fault cleared after a
// failed recovery.
func TestFailedRecoveryKeepsTheJournal(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "darwin")
	clear := leaveJournal(t, home, e)
	before := snapshot(t, home)
	if _, err := Uninstall(context.Background(), e); err == nil || !strings.Contains(err.Error(), "keys.json") {
		t.Errorf("uninstall: %v", err)
	}
	if _, err := InstallExtras(context.Background(), e, Options{Drawer: true}); err == nil || !strings.Contains(err.Error(), "keys.json") {
		t.Errorf("install --drawer: %v", err)
	}
	sameFiles(t, before, snapshot(t, home))
	if len(f.calls) != 0 {
		t.Errorf("Herdr was asked to change a layout: %v", f.calls)
	}

	clear()
	did, err := Uninstall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, home, "keys.json") != keysFile || exists(home, JournalFile) {
		t.Errorf("not recovered: %q", did)
	}
}

// Scenario: Clone unreadable.
func TestCloneUnreadable(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	f.cloneText = "{ not json"
	if _, err := InstallExtras(context.Background(), e, Options{Drawer: true}); err == nil {
		t.Fatal("install succeeded")
	}
	if shownLayout(home) != "main" || exists(home, "layouts/main-tuckerbox.json") {
		t.Errorf("clone left or not switched back: %v", f.calls)
	}
	if r := readRec(t, home); r != nil && r.Drawer != nil {
		t.Errorf("recorded a drawer: %+v", r.Drawer)
	}
	if exists(home, JournalFile) {
		t.Error("journal left")
	}
}

// Scenario: Clean-up fails.
func TestCloneCleanUpFails(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	f.cloneText = "{ not json"
	failRemoves(t, func(p string) bool { return p == layoutPath(home, "main-tuckerbox") })
	did, err := InstallExtras(context.Background(), e, Options{Drawer: true})
	if err == nil {
		t.Fatal("install succeeded")
	}
	r := readRec(t, home)
	if r == nil || r.Drawer == nil {
		t.Fatalf("clone not recorded: %q", did)
	}
	if *r.Drawer != (DrawerRecord{Layout: "main-tuckerbox", ClonedFrom: "main", Before: "", After: sum([]byte(f.cloneText))}) {
		t.Errorf("drawer %+v", r.Drawer)
	}
	if !strings.Contains(strings.Join(did, "\n"), "run tuckerbox herdr uninstall") {
		t.Errorf("output %q", did)
	}
}

// Scenario: Malformed clone removed (end to end: a failed install whose
// clean-up failed, then uninstall with Herdr running).
func TestMalformedCloneRemoved(t *testing.T) {
	home := herdrHome(t)
	e, f := extrasEnv(t, home, "linux")
	f.cloneText, f.downAfterClone = "{ not json", true
	if _, err := InstallExtras(context.Background(), e, Options{Drawer: true}); err == nil {
		t.Fatal("install succeeded")
	}
	if r := readRec(t, home); r == nil || r.Drawer == nil || shownLayout(home) != "main-tuckerbox" {
		t.Fatalf("expected the shown clone recorded: %+v", r)
	}
	f.down, f.downAfterClone = false, false
	if _, err := Uninstall(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if shownLayout(home) != "main" || exists(home, "layouts/main-tuckerbox.json") {
		t.Errorf("clone kept or still shown: %v", f.calls)
	}
}

// Scenario: Clone edited since.
func TestCloneEditedSince(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := Install(context.Background(), e, "v1", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true, Drawer: true}); err != nil {
		t.Fatal(err)
	}
	write(t, home, "layouts/main-tuckerbox.json", "{ edited, and not JSON")
	did, err := Uninstall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, home, "layouts/main-tuckerbox.json") != "{ edited, and not JSON" {
		t.Error("edited clone changed")
	}
	if !strings.Contains(strings.Join(did, "\n"), "changed since install") {
		t.Errorf("not reported: %q", did)
	}
	if read(t, home, "keys.json") != keysFile || exists(home, "mods/tuckerbox-board") {
		t.Error("key not undone or mod kept")
	}
}

// Scenarios: New key, then Uninstall after changing the key.
func TestReinstallWithAnotherKey(t *testing.T) {
	home := herdrHome(t)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true}); err != nil {
		t.Fatal(err)
	}
	did, err := InstallExtras(context.Background(), e, Options{Keys: true, Key: "f8"})
	if err != nil {
		t.Fatal(err)
	}
	keys := read(t, home, "keys.json")
	if !strings.Contains(keys, `"f8": "mod.open:tuckerbox-board"`) || strings.Contains(keys, "cmd-shift-k") {
		t.Errorf("keys.json:\n%s", keys)
	}
	if r := readRec(t, home); r.Keys.Key != "f8" || r.Keys.Before != keysFile {
		t.Errorf("record %+v", r.Keys)
	}
	if !strings.Contains(did[0], "instead of cmd-shift-k") {
		t.Errorf("did %q", did)
	}
	if _, err := Uninstall(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if read(t, home, "keys.json") != keysFile {
		t.Error("keys.json not byte for byte as before the first install")
	}
}

// Scenario: New key clashes.
func TestReinstallKeyClashes(t *testing.T) {
	home := herdrHome(t)
	write(t, home, "keys.json", `{"global": {"f8": "app.palette"}}`)
	e, _ := extrasEnv(t, home, "darwin")
	if _, err := InstallExtras(context.Background(), e, Options{Keys: true}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, home)
	_, err := InstallExtras(context.Background(), e, Options{Keys: true, Key: "f8"})
	var clash *KeyClash
	if !errors.As(err, &clash) || clash.Action != "app.palette" {
		t.Fatalf("err %v", err)
	}
	sameFiles(t, before, snapshot(t, home))
}
