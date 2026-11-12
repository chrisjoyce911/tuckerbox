package herdrmod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// LockFile, in the Herdr home, is the exclusive OS lock install and
// uninstall hold from before they read the install record until after they
// write it, so two at once can't lose each other's record. It is never
// removed: removing it would race a waiter.
const LockFile = ".tuckerbox-install.lock"

// JournalFile, in the Herdr home, holds what an unfinished install is
// changing, written before the first change: each file's bytes before and the
// hash about to be written, and each layout cloned with the layout shown
// before. It exists only while an install is unfinished, or when undoing one
// failed; the next install or uninstall finishes the undo.
const JournalFile = ".tuckerbox-install.journal.json"

// ErrBusy is returned when another install or uninstall holds the lock for
// longer than the wait. Nothing was changed.
var ErrBusy = errors.New("another tuckerbox herdr install or uninstall is running")

// lockWait and lockPoll are how long install and uninstall wait for the lock
// and how often they try. Tests shorten lockWait.
var (
	lockWait = 30 * time.Second
	lockPoll = 100 * time.Millisecond
)

// replaceFile and removeFile are how install and uninstall write and remove
// the person's Herdr files and the install record. Tests replace them to
// inject faults. The journal is written with writeDurable, never these.
var (
	replaceFile = writeAtomic
	removeFile  = os.Remove
)

// lockHome takes the install lock of home, waiting up to lockWait.
func lockHome(ctx context.Context, home string) (unlock func(), err error) {
	path := filepath.Join(home, LockFile)
	f := flock.New(path)
	wctx, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	ok, err := f.TryLockContext(wctx, lockPoll)
	if ok {
		return func() { _ = f.Unlock() }, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return nil, fmt.Errorf("%w (waited %s for %s); nothing was changed", ErrBusy, lockWait, path)
}

func journalPath(home string) string { return filepath.Join(home, JournalFile) }

type journal struct {
	Version int      `json:"version"`
	Op      string   `json:"op"`
	Entries []jentry `json:"entries"`
}

// jentry is one change: a file (File, Before, After) or a layout clone
// (Clone, From, ShownBefore, After).
type jentry struct {
	// File is the path in the Herdr home, slash-separated.
	File string
	// Before is the file's bytes before the change; nil means it was absent.
	Before *string
	// Clone is the layout install cloned from From while ShownBefore was shown.
	Clone, From, ShownBefore string
	// After is the SHA-256 of what was (about to be) written; nil for a clone
	// whose hash isn't known yet.
	After *string
}

type jentryJSON struct {
	File        string  `json:"file,omitempty"`
	Before      *string `json:"before"`
	Clone       string  `json:"clone,omitempty"`
	From        string  `json:"from,omitempty"`
	ShownBefore string  `json:"shown_before,omitempty"`
	After       *string `json:"after"`
}

func (e jentry) MarshalJSON() ([]byte, error) {
	if e.Clone != "" {
		return json.Marshal(struct {
			Clone       string  `json:"clone"`
			From        string  `json:"from"`
			ShownBefore string  `json:"shown_before"`
			After       *string `json:"after"`
		}{e.Clone, e.From, e.ShownBefore, e.After})
	}
	return json.Marshal(struct {
		File   string  `json:"file"`
		Before *string `json:"before"`
		After  *string `json:"after"`
	}{e.File, e.Before, e.After})
}

func (e *jentry) UnmarshalJSON(b []byte) error {
	var j jentryJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	*e = jentry{File: j.File, Before: j.Before, Clone: j.Clone, From: j.From, ShownBefore: j.ShownBefore, After: j.After}
	if e.File == "" && e.Clone == "" {
		return errors.New("journal entry names no file and no clone")
	}
	return nil
}

func (e jentry) name() string {
	if e.Clone != "" {
		return "layouts/" + e.Clone + ".json"
	}
	return e.File
}

func strp(s string) *string { return &s }

func readJournal(home string) (*journal, error) {
	b, err := os.ReadFile(journalPath(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var j journal
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("%s can't be read (%v); nothing was changed. It holds what an earlier install changed: fix or remove it, then run this again", journalPath(home), err)
	}
	return &j, nil
}

// saveJournal writes j durably, or removes the journal when j has no entries.
func saveJournal(home string, j *journal) error {
	path := journalPath(home)
	if len(j.Entries) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("removing the install journal %s: %w", path, err)
		}
		return nil
	}
	j.Version = 1
	b, _ := json.MarshalIndent(j, "", "  ")
	if err := writeDurable(path, append(b, '\n')); err != nil {
		return fmt.Errorf("writing the install journal %s: %w", path, err)
	}
	return nil
}

// writeDurable writes b to path through a synced temporary file renamed into
// place, then syncs the folder where the system allows it.
func writeDurable(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tuckerbox-tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // not supported on every system; the rename is what counts
		d.Close()
	}
	return nil
}

// txn is one install's changes, journaled before each is made.
type txn struct {
	ctx  context.Context
	home string
	env  Env
	j    journal
}

func newTxn(ctx context.Context, home string, env Env, op string) *txn {
	return &txn{ctx: ctx, home: home, env: env, j: journal{Version: 1, Op: op}}
}

// add journals e; on failure e is not kept and nothing has changed.
func (t *txn) add(e jentry) error {
	t.j.Entries = append(t.j.Entries, e)
	if err := saveJournal(t.home, &t.j); err != nil {
		t.j.Entries = t.j.Entries[:len(t.j.Entries)-1]
		return err
	}
	return nil
}

// write replaces the file rel in the Herdr home with after, journaling its
// bytes before and the hash of after first.
func (t *txn) write(rel string, after []byte) error {
	path := filepath.Join(t.home, filepath.FromSlash(rel))
	e := jentry{File: rel, After: strp(sum(after))}
	cur, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		e.Before = strp(string(cur))
	}
	if err := t.add(e); err != nil {
		return err
	}
	return replaceFile(path, after)
}

// clone asks the Herdr window to clone layout name as as (which also shows
// the clone), journaling the clone first and its hash once Herdr wrote it.
func (t *txn) clone(name, as string) error {
	if err := t.add(jentry{Clone: as, From: name, ShownBefore: shownLayout(t.home)}); err != nil {
		return err
	}
	if _, err := t.env.Do(t.ctx, "layout.clone", map[string]any{"name": name, "as": as}); err != nil {
		return fmt.Errorf("%w to clone the shipped layout %s (%v); open Herdr and run this again", ErrHerdrNotRunning, name, err)
	}
	// The window writes the clone; wait for it.
	path := layoutPath(t.home, as)
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			t.j.Entries[len(t.j.Entries)-1].After = strp(sum(b))
			return saveJournal(t.home, &t.j)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Herdr didn't write layouts/%s.json", as)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// undoEntry puts back one journaled change. It returns a note for the person
// ("" when there is nothing to say) and an error when the undo itself failed.
// live is true while the run that made the change is undoing it, so a clone
// whose hash wasn't journaled yet is known to be its own.
func undoEntry(ctx context.Context, home string, env Env, e jentry, live bool) (string, error) {
	if e.Clone == "" {
		path := filepath.Join(home, filepath.FromSlash(e.File))
		cur, err := os.ReadFile(path)
		absent := errors.Is(err, fs.ErrNotExist)
		if err != nil && !absent {
			return "", fmt.Errorf("reading %s: %w", e.File, err)
		}
		if (absent && e.Before == nil) || (!absent && e.Before != nil && string(cur) == *e.Before) {
			return "", nil // as it was already
		}
		if absent || e.After == nil || sum(cur) != *e.After {
			return fmt.Sprintf("%s changed since that install; left as it is", e.File), nil
		}
		if e.Before == nil {
			err = removeFile(path)
		} else {
			err = replaceFile(path, []byte(*e.Before))
		}
		if err != nil {
			return "", fmt.Errorf("putting %s back: %w", e.File, err)
		}
		return fmt.Sprintf("put %s back as it was", e.File), nil
	}
	path := layoutPath(home, e.Clone)
	cur, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil // never made, or removed already
	}
	if err != nil {
		return "", fmt.Errorf("reading layouts/%s.json: %w", e.Clone, err)
	}
	after := e.After
	if after == nil {
		if !live {
			return fmt.Sprintf("layout %s was cloned but its contents weren't recorded, so nothing proves it is unchanged; left as it is", e.Clone), nil
		}
		after = strp(sum(cur))
	}
	if sum(cur) != *after {
		return fmt.Sprintf("layout %s changed since that install; left as it is", e.Clone), nil
	}
	back := e.ShownBefore
	if back == "" || back == e.Clone {
		back = e.From
	}
	if shownLayout(home) == e.Clone {
		if _, err := env.Do(ctx, "layout.switch", map[string]any{"name": back}); err != nil {
			return "", fmt.Errorf("%w to switch back to layout %s before removing layouts/%s.json (%v)", ErrHerdrNotRunning, back, e.Clone, err)
		}
	}
	if err := removeFile(path); err != nil {
		return "", fmt.Errorf("removing layouts/%s.json: %w", e.Clone, err)
	}
	return fmt.Sprintf("removed layout %s and showed %s again", e.Clone, back), nil
}

// undoAll undoes j's entries, last first, dropping each from the journal on
// disk once undone. It stops at the first undo that fails, leaving that entry
// and every earlier one in the journal, and returns the failed entry.
func undoAll(ctx context.Context, home string, env Env, j *journal, live bool) ([]string, *jentry, error) {
	var notes []string
	for len(j.Entries) > 0 {
		e := j.Entries[len(j.Entries)-1]
		note, err := undoEntry(ctx, home, env, e, live)
		if err != nil {
			return notes, &e, err
		}
		if note != "" {
			notes = append(notes, note)
		}
		j.Entries = j.Entries[:len(j.Entries)-1]
		if err := saveJournal(home, j); err != nil {
			return notes, nil, err
		}
	}
	return notes, nil, nil
}

// recoverJournal finishes the undo of an earlier install that left its
// journal. It runs under the lock before install or uninstall changes
// anything; when it fails, the caller must change nothing else.
func recoverJournal(ctx context.Context, home string, env Env) (bool, []string, error) {
	j, err := readJournal(home)
	if err != nil || j == nil {
		return false, nil, err
	}
	notes := []string{fmt.Sprintf("an earlier install didn't finish; undoing what it changed (%s)", JournalFile)}
	more, _, err := undoAll(ctx, home, env, j, false)
	notes = append(notes, more...)
	if err != nil {
		return true, notes, fmt.Errorf("finishing the undo of an earlier install failed: %v; %s keeps what is left and nothing else was changed. Fix that, then run tuckerbox herdr uninstall or install again", err, journalPath(home))
	}
	return true, append(notes, "finished undoing the earlier install"), nil
}
