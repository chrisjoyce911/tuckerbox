package herdrmod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/herdr"
)

// RecordFile, in the Herdr home, says what `tuckerbox herdr install` changed
// there, so `tuckerbox herdr uninstall` reverses exactly that. It is hidden,
// so Herdr doesn't load or reload anything for it.
const RecordFile = ".tuckerbox-install.json"

// DrawerID is the drawer's floating block id in the person's layout.
const DrawerID = "tuckerbox-drawer"

// Record is the content of RecordFile.
type Record struct {
	Version int           `json:"version"`
	Mods    []string      `json:"mods"`
	Keys    *KeysRecord   `json:"keys,omitempty"`
	Drawer  *DrawerRecord `json:"drawer,omitempty"`
}

// KeysRecord is the binding install added to keys.json.
type KeysRecord struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	Action  string `json:"action"`
	// Before is keys.json as it was; After the SHA-256 of what install wrote.
	// While the file is still what install wrote, uninstall puts Before back
	// byte for byte; otherwise it removes only the binding.
	Before string `json:"before"`
	After  string `json:"after"`
}

// DrawerRecord is the floating block install added to a layout.
type DrawerRecord struct {
	Layout string `json:"layout"`
	// ClonedFrom is the shipped layout install cloned (layout.clone) to make
	// Layout; empty when Layout was already the person's own.
	ClonedFrom string `json:"cloned_from,omitempty"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

func recordPath(home string) string { return filepath.Join(home, RecordFile) }

func readRecord(home string) (*Record, error) {
	b, err := os.ReadFile(recordPath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", recordPath(home), err)
	}
	return &r, nil
}

func writeRecord(home string, r *Record) error {
	r.Version = 1
	b, _ := json.MarshalIndent(r, "", "  ")
	return writeAtomic(recordPath(home), append(b, '\n'))
}

func updateRecord(home string, f func(*Record)) error {
	r, err := readRecord(home)
	if err != nil {
		return err
	}
	if r == nil {
		r = &Record{}
	}
	f(r)
	return writeRecord(home, r)
}

func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tuckerbox-tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

func herdrAppDo(ctx context.Context, command string, args map[string]any) (string, error) {
	bin, err := exec.LookPath("herdr-app")
	if err != nil {
		return "", fmt.Errorf("%w on PATH", ErrNoChecker)
	}
	a, _ := json.Marshal(args)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "do", command, string(a)).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("herdr-app do %s: %s", command, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (e Env) goos() string {
	if e.GOOS != "" {
		return e.GOOS
	}
	return runtime.GOOS
}

// ---- keys ----

// ErrNoKeysFile is returned when Herdr hasn't written keys.json yet.
var ErrNoKeysFile = errors.New("no keys.json in the Herdr app folder yet: open Herdr once, then run this again")

// KeyClash is returned when the toggle key is already bound: nothing is changed.
type KeyClash struct{ Key, Section, Action string }

func (c *KeyClash) Error() string {
	return fmt.Sprintf("%s is already bound to %s in keys.json's %q section; left as it is (choose another key with --key)", c.Key, c.Action, c.Section)
}

// ToggleBinding is the binding the mod ships in its keys.json: its key as
// written there (Linux and Windows spelling; on macOS ctrl- becomes cmd-, as
// Herdr's own shortcuts do) and its action, mod.open of the board.
func ToggleBinding(env Env) (key, action string, err error) {
	mod, err := env.mod()
	if err != nil {
		return "", "", err
	}
	b, err := fs.ReadFile(mod, "keys.json")
	if err != nil {
		return "", "", err
	}
	var k map[string]map[string]string
	if err := json.Unmarshal(b, &k); err != nil {
		return "", "", fmt.Errorf("the mod's keys.json: %w", err)
	}
	for key, action := range k["global"] {
		if env.goos() == "darwin" && strings.HasPrefix(key, "ctrl-") {
			key = "cmd-" + strings.TrimPrefix(key, "ctrl-")
		}
		return key, action, nil
	}
	return "", "", errors.New("the mod's keys.json binds no global key")
}

// normKey spells a key so equal keys compare equal: lower case, modifiers
// sorted ("shift-ctrl-k" and "ctrl-shift-k" are one key).
func normKey(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	last := k
	mods := ""
	if strings.HasSuffix(k, "--") { // the "-" key itself
		last, mods = "-", strings.TrimSuffix(k, "--")
	} else if i := strings.LastIndex(k, "-"); i > 0 {
		last, mods = k[i+1:], k[:i]
	}
	var ms []string
	if mods != "" {
		ms = strings.Split(mods, "-")
	}
	sort.Strings(ms)
	return strings.Join(append(ms, last), "-")
}

func actionText(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case *obj:
		if c, ok := v.get("command"); ok {
			if s, ok := c.(string); ok {
				return s
			}
		}
	}
	return "an action"
}

// installKeys adds the toggle binding to the person's keys.json, unless that
// key is bound already in any section a key press can reach (not a mod:
// section). It returns what it did in words.
func installKeys(home string, env Env, rec *Record, key string) (string, error) {
	shippedKey, action, err := ToggleBinding(env)
	if err != nil {
		return "", err
	}
	if key == "" {
		key = shippedKey
	}
	path := filepath.Join(home, "keys.json")
	before, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNoKeysFile
	}
	if err != nil {
		return "", err
	}
	v, err := parseOrdered(before)
	if err != nil {
		return "", fmt.Errorf("keys.json: %w", err)
	}
	root, ok := v.(*obj)
	if !ok {
		return "", errors.New("keys.json is not a JSON object")
	}
	if rec.Keys != nil {
		if g, ok := root.get(rec.Keys.Section); ok {
			if s, ok := g.(*obj); ok {
				if a, ok := s.get(rec.Keys.Key); ok && actionText(a) == rec.Keys.Action {
					return fmt.Sprintf("%s already opens and hides the board", rec.Keys.Key), nil
				}
			}
		}
	}
	want := normKey(key)
	for _, sec := range root.keys {
		if strings.HasPrefix(sec, "mod:") {
			continue
		}
		s, ok := root.vals[sec].(*obj)
		if !ok {
			continue
		}
		for _, k := range s.keys {
			if normKey(k) != want {
				continue
			}
			if sec == "global" && actionText(s.vals[k]) == action {
				return fmt.Sprintf("%s already opens and hides the board (bound before install; left as it is)", k), nil
			}
			return "", &KeyClash{Key: k, Section: sec, Action: actionText(s.vals[k])}
		}
	}
	g, ok := root.get("global")
	global, isObj := g.(*obj)
	if !ok || !isObj {
		if ok {
			return "", errors.New(`keys.json's "global" is not an object`)
		}
		global = newObj()
		root.keys = append([]string{"global"}, root.keys...)
		root.vals["global"] = global
	}
	global.set(key, action)
	after := encodeOrdered(root)
	if err := writeAtomic(path, after); err != nil {
		return "", err
	}
	rec.Keys = &KeysRecord{Section: "global", Key: key, Action: action, Before: string(before), After: sum(after)}
	return fmt.Sprintf("%s now opens and hides the board (keys.json is now edited, so Herdr no longer adds new default shortcuts to it; tuckerbox herdr uninstall puts it back)", key), nil
}

func uninstallKeys(home string, k *KeysRecord) (string, error) {
	path := filepath.Join(home, "keys.json")
	cur, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "keys.json is gone; nothing to undo there", nil
	}
	if err != nil {
		return "", err
	}
	if sum(cur) == k.After {
		if err := writeAtomic(path, []byte(k.Before)); err != nil {
			return "", err
		}
		return "keys.json put back as it was before install", nil
	}
	v, err := parseOrdered(cur)
	if err != nil {
		return "", fmt.Errorf("keys.json: %w", err)
	}
	root, _ := v.(*obj)
	if root != nil {
		if s, ok := root.vals[k.Section].(*obj); ok {
			if a, ok := s.get(k.Key); ok && actionText(a) == k.Action {
				s.del(k.Key)
				if err := writeAtomic(path, encodeOrdered(root)); err != nil {
					return "", err
				}
				return fmt.Sprintf("removed %s from keys.json (the file changed since install, so the rest stays)", k.Key), nil
			}
		}
	}
	return fmt.Sprintf("%s no longer opens the board in keys.json; left as it is", k.Key), nil
}

// ---- drawer ----

// ErrHerdrNotRunning is returned when a step needs the Herdr window
// (layout.clone, layout.switch) and herdr-app do can't reach it.
var ErrHerdrNotRunning = errors.New("this needs the Herdr app running")

func shownLayout(home string) string {
	b, err := os.ReadFile(filepath.Join(home, "shared.json"))
	if err == nil {
		var s map[string]any
		if json.Unmarshal(b, &s) == nil {
			if l, ok := s["layout"].(string); ok && l != "" {
				return l
			}
		}
	}
	return "main"
}

func shippedLayout(home, name string) bool {
	b, err := os.ReadFile(filepath.Join(home, "installed.json"))
	if err != nil {
		return false
	}
	var s map[string]json.RawMessage
	if json.Unmarshal(b, &s) != nil {
		return false
	}
	_, ok := s["layout:"+name]
	return ok
}

func layoutPath(home, name string) string { return filepath.Join(home, "layouts", name+".json") }

// drawerBlock is the floating block: the board mod's drawer view, a peek
// drawer on the right edge, low down.
func drawerBlock() *obj {
	props := newObj()
	props.set("view", "drawer")
	content := newObj()
	content.set("mod", herdr.ModName)
	content.set("props", props)
	b := newObj()
	b.set("id", DrawerID)
	b.set("content", content)
	b.set("edge", "right")
	b.set("align", "end")
	b.set("peek", json.Number("48"))
	return b
}

func hasDrawer(root *obj) bool {
	f, _ := root.vals["floating"].([]any)
	for _, x := range f {
		if o, ok := x.(*obj); ok {
			if id, _ := o.vals["id"].(string); id == DrawerID {
				return true
			}
		}
	}
	return false
}

func readLayout(path string) ([]byte, *obj, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	v, err := parseOrdered(b)
	if err != nil {
		return b, nil, fmt.Errorf("%s: %w", path, err)
	}
	root, ok := v.(*obj)
	if !ok {
		return b, nil, fmt.Errorf("%s is not a JSON object", path)
	}
	return b, root, nil
}

// installDrawer adds the drawer to the shown layout's root floating list. A
// shipped layout is read-only, so it is cloned first (layout.clone, which
// also switches to the clone) and the clone gets the drawer.
func installDrawer(ctx context.Context, home string, env Env, rec *Record) (string, error) {
	if rec.Drawer != nil {
		if _, root, err := readLayout(layoutPath(home, rec.Drawer.Layout)); err == nil && hasDrawer(root) {
			return fmt.Sprintf("the drawer is already in layout %s", rec.Drawer.Layout), nil
		}
	}
	name := shownLayout(home)
	d := &DrawerRecord{Layout: name}
	if shippedLayout(home, name) {
		as := name + "-tuckerbox"
		for i := 2; ; i++ {
			if _, err := os.Stat(layoutPath(home, as)); errors.Is(err, fs.ErrNotExist) {
				break
			}
			as = fmt.Sprintf("%s-tuckerbox-%d", name, i)
		}
		if _, err := env.Do(ctx, "layout.clone", map[string]any{"name": name, "as": as}); err != nil {
			return "", fmt.Errorf("%w to clone the shipped layout %s (%v); open Herdr and run this again", ErrHerdrNotRunning, name, err)
		}
		// The window writes the clone; wait for it.
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(layoutPath(home, as)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return "", fmt.Errorf("Herdr didn't write layouts/%s.json", as)
			}
			time.Sleep(100 * time.Millisecond)
		}
		d.Layout, d.ClonedFrom = as, name
	}
	path := layoutPath(home, d.Layout)
	before, root, err := readLayout(path)
	if err != nil {
		return "", err
	}
	if hasDrawer(root) {
		return fmt.Sprintf("layout %s already has a %s block; left as it is", d.Layout, DrawerID), nil
	}
	f, _ := root.vals["floating"].([]any)
	root.set("floating", append(f, drawerBlock()))
	after := encodeOrdered(root)
	if err := writeAtomic(path, after); err != nil {
		return "", err
	}
	d.Before, d.After = string(before), sum(after)
	rec.Drawer = d
	if d.ClonedFrom != "" {
		return fmt.Sprintf("added the drawer to layout %s, a copy of Herdr's %s (now shown)", d.Layout, d.ClonedFrom), nil
	}
	return fmt.Sprintf("added the drawer to your layout %s", d.Layout), nil
}

func uninstallDrawer(ctx context.Context, home string, env Env, d *DrawerRecord) (string, error) {
	path := layoutPath(home, d.Layout)
	cur, root, err := readLayout(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Sprintf("layout %s is gone; nothing to undo there", d.Layout), nil
	}
	if err != nil {
		return "", err
	}
	unchanged := sum(cur) == d.After
	if d.ClonedFrom != "" && unchanged {
		// The clone is still just install's: switch back if it shows, then remove it.
		switched := ""
		if shownLayout(home) == d.Layout {
			if _, err := env.Do(ctx, "layout.switch", map[string]any{"name": d.ClonedFrom}); err != nil {
				return "", fmt.Errorf("%w to switch back to layout %s (%v); open Herdr and run this again", ErrHerdrNotRunning, d.ClonedFrom, err)
			}
			switched = fmt.Sprintf(" and switched back to %s", d.ClonedFrom)
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
		return fmt.Sprintf("removed layout %s (install's copy of %s)%s", d.Layout, d.ClonedFrom, switched), nil
	}
	if unchanged {
		if err := writeAtomic(path, []byte(d.Before)); err != nil {
			return "", err
		}
		return fmt.Sprintf("layout %s put back as it was before install", d.Layout), nil
	}
	if !hasDrawer(root) {
		return fmt.Sprintf("layout %s no longer has the drawer; left as it is", d.Layout), nil
	}
	f, _ := root.vals["floating"].([]any)
	var keep []any
	for _, x := range f {
		if o, ok := x.(*obj); ok {
			if id, _ := o.vals["id"].(string); id == DrawerID {
				continue
			}
		}
		keep = append(keep, x)
	}
	if len(keep) == 0 {
		root.del("floating")
	} else {
		root.set("floating", keep)
	}
	if err := writeAtomic(path, encodeOrdered(root)); err != nil {
		return "", err
	}
	if d.ClonedFrom != "" {
		return fmt.Sprintf("removed the drawer from layout %s; kept the layout, since it changed since install", d.Layout), nil
	}
	return fmt.Sprintf("removed the drawer from layout %s (it changed since install, so the rest stays)", d.Layout), nil
}

// ---- install extras, uninstall ----

// Options are install's opt-in changes beyond copying the mod.
type Options struct {
	Keys   bool   // bind the toggle key (the mod's keys.json)
	Key    string // a key other than the shipped one
	Drawer bool   // add the drawer to the shown layout
}

// InstallExtras makes the opt-in changes, after Install, and records each
// one as it is made. It returns what it did, line by line; on an error the
// lines say what was done before it.
func InstallExtras(ctx context.Context, env Env, opt Options) ([]string, error) {
	env = env.withDefaults()
	home, err := Home(env)
	if err != nil {
		return nil, err
	}
	rec, err := readRecord(home)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		rec = &Record{}
	}
	var did []string
	var clash *KeyClash
	if opt.Keys {
		msg, err := installKeys(home, env, rec, opt.Key)
		switch {
		case errors.As(err, &clash):
			// Reported at the end; the drawer doesn't depend on the key.
		case err != nil:
			return did, err
		default:
			did = append(did, msg)
			if err := writeRecord(home, rec); err != nil {
				return did, err
			}
		}
	}
	if opt.Drawer {
		msg, err := installDrawer(ctx, home, env, rec)
		if err != nil {
			return did, err
		}
		did = append(did, msg)
		if err := writeRecord(home, rec); err != nil {
			return did, err
		}
	}
	if clash != nil {
		return did, clash
	}
	return did, nil
}

// ErrNothingInstalled is returned by Uninstall when there is no record and no mod.
var ErrNothingInstalled = errors.New("Tuckerbox's board isn't installed in Herdr")

// Uninstall reverses what the install record says install did: the drawer,
// the key, then the mod folders, and last the record. A file changed since
// install loses only Tuckerbox's entry. Without a record it removes the
// board mod folder only.
func Uninstall(ctx context.Context, env Env) ([]string, error) {
	env = env.withDefaults()
	home, err := Home(env)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: no Herdr app folder at %s", ErrNoHerdr, home)
	}
	rec, err := readRecord(home)
	if err != nil {
		return nil, err
	}
	var did []string
	if rec == nil {
		dir := filepath.Join(home, "mods", herdr.ModName)
		if _, err := os.Stat(filepath.Join(dir, VersionFile)); err != nil {
			return nil, ErrNothingInstalled
		}
		if err := os.RemoveAll(dir); err != nil {
			return nil, err
		}
		return []string{"removed " + dir + " (no install record, so nothing else was changed)"}, nil
	}
	if rec.Drawer != nil {
		msg, err := uninstallDrawer(ctx, home, env, rec.Drawer)
		if err != nil {
			return did, err
		}
		did = append(did, msg)
		rec.Drawer = nil
		if err := writeRecord(home, rec); err != nil {
			return did, err
		}
	}
	if rec.Keys != nil {
		msg, err := uninstallKeys(home, rec.Keys)
		if err != nil {
			return did, err
		}
		did = append(did, msg)
		rec.Keys = nil
		if err := writeRecord(home, rec); err != nil {
			return did, err
		}
	}
	for _, m := range rec.Mods {
		if m == "" || strings.ContainsAny(m, `/\`) || m == "." || m == ".." {
			continue
		}
		dir := filepath.Join(home, "mods", m)
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return did, err
		}
		did = append(did, "removed "+dir)
	}
	if err := os.Remove(recordPath(home)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return did, err
	}
	return did, nil
}
