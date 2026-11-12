// Package herdrmod installs the Herdr board mod Tuckerbox ships into the
// Herdr app's mods folder (`tuckerbox herdr install`), and tells doctor
// whether the installed copy is the one this binary ships.
package herdrmod

import (
	"bytes"
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
	"sort"
	"time"

	"github.com/chrisjoyce911/tuckerbox/herdr"
)

// VersionFile is written into the installed mod: which binary installed it,
// and a digest of the files it installed.
const VersionFile = "version.json"

// ErrNoHerdr is returned when the Herdr app's home folder doesn't exist.
var ErrNoHerdr = errors.New("Herdr wasn't found")

// ErrNoChecker is returned by Env.Check when herdr-app isn't installed.
var ErrNoChecker = errors.New("herdr-app not found")

// Env is how the installer finds Herdr and checks a mod. Tests replace it.
type Env struct {
	Getenv   func(string) string
	UserHome func() (string, error)
	// Check runs `herdr-app check <dir>` and returns its output. It returns
	// an error wrapping ErrNoChecker when herdr-app isn't installed.
	Check func(ctx context.Context, dir string) (string, error)
	// Mod is the mod to install, with its files at its root; nil means the
	// one this binary embeds.
	Mod fs.FS
	// Do runs `herdr-app do <command> <args>` against the running window
	// (layout.clone and layout.switch for the drawer). Tests replace it.
	Do func(ctx context.Context, command string, args map[string]any) (string, error)
	// GOOS picks the toggle key's spelling (cmd- on macOS); "" is this one.
	GOOS string
}

// SystemEnv uses the real environment and herdr-app.
func SystemEnv() Env {
	return Env{Getenv: os.Getenv, UserHome: os.UserHomeDir, Check: herdrAppCheck, Do: herdrAppDo}
}

func herdrAppCheck(ctx context.Context, dir string) (string, error) {
	bin, err := exec.LookPath("herdr-app")
	if err != nil {
		return "", fmt.Errorf("%w on PATH", ErrNoChecker)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "check", dir).CombinedOutput()
	return string(out), err
}

func (e Env) withDefaults() Env {
	s := SystemEnv()
	if e.Getenv == nil {
		e.Getenv = s.Getenv
	}
	if e.UserHome == nil {
		e.UserHome = s.UserHome
	}
	if e.Check == nil {
		e.Check = s.Check
	}
	if e.Do == nil {
		e.Do = s.Do
	}
	return e
}

func (e Env) mod() (fs.FS, error) {
	if e.Mod != nil {
		return e.Mod, nil
	}
	return fs.Sub(herdr.Mod, herdr.ModRoot)
}

// Home is the Herdr app's home folder: $HERDR_APP_HOME, else
// ~/.config/herdr-app (on every platform, as Herdr itself uses).
func Home(env Env) (string, error) {
	env = env.withDefaults()
	if h := env.Getenv("HERDR_APP_HOME"); h != "" {
		return h, nil
	}
	u, err := env.UserHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".config", "herdr-app"), nil
}

// Stamp is the content of version.json.
type Stamp struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// Digest is the SHA-256 over the mod's files, by path; see Digest.
	Digest string `json:"digest"`
}

// Digest is a SHA-256 over every file of mod except version.json, in path
// order, each as its path, its length and its bytes.
func Digest(mod fs.FS) (string, error) {
	var names []string
	err := fs.WalkDir(mod, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && p != VersionFile {
			names = append(names, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		b, err := fs.ReadFile(mod, n)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(b))
		h.Write(b)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// Result says what Install did.
type Result struct {
	Dir      string // the installed mod folder
	Updated  bool   // an earlier copy was replaced
	Checked  bool   // herdr-app check ran and passed
	CheckOut string // its output
}

// CheckError is returned when herdr-app check rejects the mod; nothing was
// installed or changed.
type CheckError struct{ Output string }

func (e *CheckError) Error() string { return "herdr-app check failed:\n" + e.Output }

// Install copies the mod into <home>/mods/tuckerbox-board, after herdr-app
// check passes on a staged copy. Without a Herdr home it writes nothing and
// returns ErrNoHerdr. Without herdr-app it installs and says the check didn't
// run. Run again, it replaces the earlier copy whole.
func Install(ctx context.Context, env Env, version, commit string) (Result, error) {
	env = env.withDefaults()
	home, err := Home(env)
	if err != nil {
		return Result{}, err
	}
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return Result{}, fmt.Errorf("%w: no Herdr app folder at %s", ErrNoHerdr, home)
	}
	mod, err := env.mod()
	if err != nil {
		return Result{}, err
	}
	digest, err := Digest(mod)
	if err != nil {
		return Result{}, err
	}
	// Stage next to mods/, on the same file system, under a hidden name the
	// app doesn't load as a mod.
	stage, err := os.MkdirTemp(home, ".tuckerbox-board-new-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(stage)
	if err := copyFS(stage, mod); err != nil {
		return Result{}, err
	}
	stamp, _ := json.MarshalIndent(Stamp{Version: version, Commit: commit, Digest: digest}, "", "  ")
	if err := os.WriteFile(filepath.Join(stage, VersionFile), append(stamp, '\n'), 0o644); err != nil {
		return Result{}, err
	}

	res := Result{Dir: filepath.Join(home, "mods", herdr.ModName)}
	out, err := env.Check(ctx, stage)
	switch {
	case errors.Is(err, ErrNoChecker):
	case err != nil:
		return res, &CheckError{Output: out}
	default:
		res.Checked, res.CheckOut = true, out
	}
	// The check may write type files (gpui-kit.d.ts, jsconfig.json) beside
	// the mod; the app writes its own on start.
	for _, n := range []string{"gpui-kit.d.ts", "jsconfig.json"} {
		if _, err := fs.Stat(mod, n); err != nil {
			os.Remove(filepath.Join(stage, n))
		}
	}

	if err := os.MkdirAll(filepath.Dir(res.Dir), 0o755); err != nil {
		return res, err
	}
	old := ""
	if _, err := os.Stat(res.Dir); err == nil {
		res.Updated = true
		old = stage + ".old"
		if err := os.Rename(res.Dir, old); err != nil {
			return res, err
		}
	}
	if err := os.Rename(stage, res.Dir); err != nil {
		if old != "" {
			os.Rename(old, res.Dir)
		}
		return res, err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	// The record lets tuckerbox herdr uninstall remove exactly this.
	err = updateRecord(home, func(r *Record) {
		for _, m := range r.Mods {
			if m == herdr.ModName {
				return
			}
		}
		r.Mods = append(r.Mods, herdr.ModName)
	})
	return res, err
}

func copyFS(dst string, src fs.FS) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}

// State is what doctor reports about the installed mod.
type State struct {
	Herdr     bool  // the Herdr app's home exists
	Installed bool  // the mod folder exists
	Current   bool  // it is the copy this binary ships, unchanged
	Stamp     Stamp // what its version.json says (empty if unreadable)
	Dir       string
	// Shipped files that differ from this binary's, or are gone, by
	// slash-separated path. Other files are ignored: Herdr writes its own
	// (gpui-kit.d.ts, jsconfig.json) and a person may add a dev.json.
	Modified, Missing []string
}

// Inspect reports whether the mod is installed and whether it is the one
// this binary ships: its version.json names this binary's mod, and every
// shipped file is there with the shipped bytes.
func Inspect(env Env) (State, error) {
	env = env.withDefaults()
	home, err := Home(env)
	if err != nil {
		return State{}, err
	}
	s := State{Dir: filepath.Join(home, "mods", herdr.ModName)}
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return s, nil
	}
	s.Herdr = true
	if st, err := os.Stat(s.Dir); err != nil || !st.IsDir() {
		return s, nil
	}
	s.Installed = true
	b, err := os.ReadFile(filepath.Join(s.Dir, VersionFile))
	if err == nil {
		_ = json.Unmarshal(bytes.TrimSpace(b), &s.Stamp)
	}
	mod, err := env.mod()
	if err != nil {
		return s, err
	}
	want, err := Digest(mod)
	if err != nil {
		return s, err
	}
	err = fs.WalkDir(mod, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		shipped, err := fs.ReadFile(mod, p)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(p)))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			s.Missing = append(s.Missing, p)
		case err != nil:
			return err
		case !bytes.Equal(got, shipped):
			s.Modified = append(s.Modified, p)
		}
		return nil
	})
	if err != nil {
		return s, err
	}
	s.Current = s.Stamp.Digest == want && len(s.Modified) == 0 && len(s.Missing) == 0
	return s, nil
}
