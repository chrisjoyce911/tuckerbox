package approval

// Approvals published on the repository's remote (openspec change
// fix-approval-publish, capability approval-publish).
//
// Each approve or unapprove is one commit on refs/tuckerbox/approvals whose
// tree holds approvals/<change>.toml. The commit is pushed at once with
// --force-with-lease on the remote's version fetched just before; a refused
// push fetches and applies its change again (formal/approvals.tla,
// NoLostApproval). The remote's version is kept locally in RemoteRef; the
// local RefName is that version plus this machine's unpublished operations,
// listed in <git-common-dir>/tuckerbox/approvals-pending.json.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	// RefName is the approvals ref, on the remote and locally.
	RefName = "refs/tuckerbox/approvals"
	// RemoteRef is this machine's copy of the remote's RefName.
	RemoteRef = "refs/tuckerbox/approvals-remote"
	// Remote is the remote approvals are published to.
	Remote = "origin"
	// PushTries bounds the fetch-and-push loop when other machines push first.
	PushTries = 5
)

// Entry states.
const (
	EntryApproved  = "approved"
	EntryWithdrawn = "withdrawn"
)

// Entry is one change's file on the approvals ref.
type Entry struct {
	State string `toml:"state" json:"state"`
	Hash  string `toml:"hash,omitempty" json:"hash,omitempty"`
	// SettingsHash and Settings are the approved agent and permissions
	// overrides (see [Approval]); a withdrawal has neither. An entry
	// without a settings hash approved no overrides.
	SettingsHash string    `toml:"settings_hash,omitempty" json:"settings_hash,omitempty"`
	Settings     []string  `toml:"settings,omitempty" json:"settings,omitempty"`
	By           string    `toml:"by" json:"by"`
	At           time.Time `toml:"at" json:"at"`
}

// ErrOffline: the remote could not be reached; the operation stays on the
// local ref and counts on this machine until it is published.
var ErrOffline = errors.New("the remote could not be reached; the approval is recorded on this machine and will be published with the next approve, unapprove or board read")

// Store is the approvals ref of the repository containing Dir.
type Store struct {
	Dir string
	// Now and Sleep are for tests; nil means the real clock.
	Now   func() time.Time
	Sleep func(time.Duration)
	// beforePush, for tests, runs between building a commit and pushing it.
	beforePush func()
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Store) git(ctx context.Context, stdin []byte, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", s.Dir}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// HasRemote reports whether the repository has the Remote to publish to.
func (s Store) HasRemote(ctx context.Context) bool {
	_, err := s.git(ctx, nil, nil, "remote", "get-url", Remote)
	return err == nil
}

func (s Store) stateDir(ctx context.Context) (string, error) {
	out, err := s.git(ctx, nil, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(out), "tuckerbox"), nil
}

type pendingOp struct {
	Change string `json:"change"`
	Entry  Entry  `json:"entry"`
}

func (s Store) pendingPath(ctx context.Context) (string, error) {
	d, err := s.stateDir(ctx)
	return filepath.Join(d, "approvals-pending.json"), err
}

func (s Store) readPending(ctx context.Context) ([]pendingOp, error) {
	p, err := s.pendingPath(ctx)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ops []pendingOp
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return ops, nil
}

func (s Store) writePending(ctx context.Context, ops []pendingOp) error {
	p, err := s.pendingPath(ctx)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(ops, "", "  ")
	return writeAtomic(p, append(data, '\n'))
}

// Unpublished lists the changes with an approve or unapprove on this
// machine that is not on the remote yet.
func (s Store) Unpublished(ctx context.Context) (map[string]bool, error) {
	ops, err := s.readPending(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, op := range ops {
		out[op.Change] = true
	}
	return out, nil
}

func (s Store) resolve(ctx context.Context, ref string) string {
	out, err := s.git(ctx, nil, nil, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Entries reads every change's entry on the local approvals ref.
func (s Store) Entries(ctx context.Context) (map[string]Entry, error) {
	out := map[string]Entry{}
	tip := s.resolve(ctx, RefName)
	if tip == "" {
		return out, nil
	}
	files, err := s.files(ctx, tip)
	if err != nil {
		return nil, err
	}
	for name, blob := range files {
		data, err := s.git(ctx, nil, nil, "cat-file", "blob", blob)
		if err != nil {
			return nil, err
		}
		var e Entry
		if _, err := toml.Decode(data, &e); err != nil {
			return nil, fmt.Errorf("%s:approvals/%s.toml: %w", RefName, name, err)
		}
		out[name] = e
	}
	return out, nil
}

// files maps change ids to blob ids in commit's approvals/ folder.
func (s Store) files(ctx context.Context, commit string) (map[string]string, error) {
	out, err := s.git(ctx, nil, nil, "ls-tree", "-z", commit, "approvals/")
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, rec := range strings.Split(out, "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		name := strings.TrimSuffix(strings.TrimPrefix(path, "approvals/"), ".toml")
		if len(f) == 3 && f[1] == "blob" && name != "" {
			files[name] = f[2]
		}
	}
	return files, nil
}

// commit makes a commit on parent ("" for none) with ops applied to its
// approvals/ folder, and returns its id.
func (s Store) commit(ctx context.Context, parent string, ops []pendingOp) (string, error) {
	files := map[string]string{}
	if parent != "" {
		var err error
		if files, err = s.files(ctx, parent); err != nil {
			return "", err
		}
	}
	var msg []string
	for _, op := range ops {
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(op.Entry); err != nil {
			return "", err
		}
		blob, err := s.git(ctx, buf.Bytes(), nil, "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		files[op.Change] = strings.TrimSpace(blob)
		verb := "Approve"
		if op.Entry.State == EntryWithdrawn {
			verb = "Unapprove"
		}
		msg = append(msg, fmt.Sprintf("%s %s (%s)", verb, op.Change, op.Entry.By))
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var sub bytes.Buffer
	for _, n := range names {
		fmt.Fprintf(&sub, "100644 blob %s\t%s.toml\n", files[n], n)
	}
	subTree, err := s.git(ctx, sub.Bytes(), nil, "mktree")
	if err != nil {
		return "", err
	}
	root, err := s.git(ctx, []byte("040000 tree "+strings.TrimSpace(subTree)+"\tapprovals\n"), nil, "mktree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", strings.TrimSpace(root), "-m", strings.Join(msg, "\n")}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	by := "tuckerbox"
	if len(ops) > 0 && ops[len(ops)-1].Entry.By != "" {
		by = ops[len(ops)-1].Entry.By
	}
	env := []string{"GIT_AUTHOR_NAME=" + by, "GIT_COMMITTER_NAME=" + by,
		"GIT_AUTHOR_EMAIL=tuckerbox@invalid", "GIT_COMMITTER_EMAIL=tuckerbox@invalid"}
	out, err := s.git(ctx, nil, env, args...)
	return strings.TrimSpace(out), err
}

func (s Store) setRef(ctx context.Context, ref, commit string) error {
	if commit == "" {
		_, err := s.git(ctx, nil, nil, "update-ref", "-d", ref)
		return err
	}
	_, err := s.git(ctx, nil, nil, "update-ref", ref, commit)
	return err
}

// Record adds one change's entry on the local ref, as an unpublished
// operation, then publishes. It returns ErrOffline when the remote could
// not be reached: the entry then counts on this machine only.
func (s Store) Record(ctx context.Context, change string, e Entry) error {
	e.At = e.At.Truncate(time.Second)
	if !s.HasRemote(ctx) {
		// Nothing to publish to: the local ref is the only copy.
		c, err := s.commit(ctx, s.resolve(ctx, RefName), []pendingOp{{Change: change, Entry: e}})
		if err != nil {
			return err
		}
		return s.setRef(ctx, RefName, c)
	}
	ops, err := s.readPending(ctx)
	if err != nil {
		return err
	}
	ops = append(ops, pendingOp{Change: change, Entry: e})
	if err := s.writePending(ctx, ops); err != nil {
		return err
	}
	if err := s.rebuild(ctx, ops); err != nil {
		return err
	}
	return s.Publish(ctx)
}

// rebuild sets the local ref to the remote's version plus ops.
func (s Store) rebuild(ctx context.Context, ops []pendingOp) error {
	base := s.resolve(ctx, RemoteRef)
	if len(ops) == 0 {
		return s.setRef(ctx, RefName, base)
	}
	c, err := s.commit(ctx, base, ops)
	if err != nil {
		return err
	}
	return s.setRef(ctx, RefName, c)
}

// fetch copies the remote's approvals ref into RemoteRef. A remote without
// the ref leaves RemoteRef deleted.
func (s Store) fetch(ctx context.Context) error {
	out, err := s.git(ctx, nil, nil, "ls-remote", Remote, RefName)
	if err != nil {
		return ErrOffline
	}
	if strings.TrimSpace(out) == "" {
		return s.setRef(ctx, RemoteRef, "")
	}
	if _, err := s.git(ctx, nil, nil, "fetch", "--quiet", "--no-tags", Remote, "+"+RefName+":"+RemoteRef); err != nil {
		return ErrOffline
	}
	return nil
}

// Publish pushes this machine's unpublished operations, applied to the
// remote's latest version, with --force-with-lease on that version. A
// refused push (someone else pushed first) fetches and tries again, up to
// PushTries times. Without a remote, operations stay local and count as
// published. It returns ErrOffline when the remote can't be reached.
func (s Store) Publish(ctx context.Context) error {
	ops, err := s.readPending(ctx)
	if err != nil || len(ops) == 0 {
		return err
	}
	for try := 1; ; try++ {
		if err := s.fetch(ctx); err != nil {
			return err
		}
		base := s.resolve(ctx, RemoteRef)
		c, err := s.commit(ctx, base, ops)
		if err != nil {
			return err
		}
		if s.beforePush != nil {
			s.beforePush()
		}
		lease := "--force-with-lease=" + RefName + ":" + base
		out, perr := s.git(ctx, nil, nil, "push", "--porcelain", lease, Remote, c+":"+RefName)
		if perr == nil {
			if err := s.setRef(ctx, RemoteRef, c); err != nil {
				return err
			}
			if err := s.setRef(ctx, RefName, c); err != nil {
				return err
			}
			return s.writePending(ctx, nil)
		}
		if !leaseRefused(out + perr.Error()) {
			_ = s.rebuild(ctx, ops)
			return ErrOffline
		}
		if try >= PushTries {
			_ = s.rebuild(ctx, ops)
			return fmt.Errorf("publishing approvals: the remote's %s kept moving (%d tries); it stays on this machine and is published next time", RefName, try)
		}
		d := time.Duration(50+rand.IntN(200)) * time.Millisecond
		if s.Sleep != nil {
			s.Sleep(d)
		} else {
			time.Sleep(d)
		}
	}
}

// leaseRefused tells a push refused because the ref moved from one that
// failed for any other reason.
func leaseRefused(out string) bool {
	return strings.Contains(out, "stale info") || strings.Contains(out, "[rejected]") ||
		strings.Contains(out, "fetch first") || strings.Contains(out, "non-fast-forward")
}

// Sync brings the local ref up to date. With unpublished operations it
// always tries to publish them. Otherwise it fetches the remote's approvals
// when the last successful fetch is older than maxAge (0: always). Being
// offline is not an error: the local ref is used as it is.
func (s Store) Sync(ctx context.Context, maxAge time.Duration) error {
	if !s.HasRemote(ctx) {
		return nil
	}
	dir, err := s.stateDir(ctx)
	if err != nil {
		return err
	}
	stamp := filepath.Join(dir, "approvals-fetched")
	ops, err := s.readPending(ctx)
	if err != nil {
		return err
	}
	if len(ops) > 0 {
		err = s.Publish(ctx)
	} else {
		if maxAge > 0 {
			if fi, err := os.Stat(stamp); err == nil && s.now().Sub(fi.ModTime()) < maxAge {
				return nil
			}
		}
		if err = s.fetch(ctx); err == nil {
			err = s.rebuild(ctx, nil)
		}
	}
	if errors.Is(err, ErrOffline) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		now := s.now()
		_ = os.WriteFile(stamp, nil, 0o644)
		_ = os.Chtimes(stamp, now, now)
	}
	return nil
}

// JudgeWith returns a change's approval state from its ref entry when it
// has one (a withdrawal is None), else from the [approval] in its
// tuckerbox.toml, as Judge does.
func JudgeWith(files Files, e *Entry) (Status, error) {
	if e == nil {
		return Judge(files)
	}
	hash, err := Hash(files)
	if err != nil {
		return Status{}, err
	}
	lines, shash, err := Settings(files)
	if err != nil {
		return Status{}, err
	}
	st := Status{State: None, Hash: hash, SettingsHash: shash, FromRef: true}
	if e.State != EntryApproved {
		return st, nil
	}
	st.Approval = &Approval{Hash: e.Hash, SettingsHash: e.SettingsHash, Settings: e.Settings, By: e.By, At: e.At}
	judgeSettings(&st, e.Hash, e.SettingsHash, e.Settings, lines)
	return st, nil
}

// CheckRemote reports whether the remote's approvals ref can be read and
// pushed: ls-remote, then a dry-run push of the local ref (or of an empty
// commit when there is none yet). It changes nothing on the remote.
func (s Store) CheckRemote(ctx context.Context) error {
	if _, err := s.git(ctx, nil, nil, "ls-remote", Remote, RefName); err != nil {
		return fmt.Errorf("can't read %s from %s: %v", RefName, Remote, err)
	}
	c := s.resolve(ctx, RefName)
	if c == "" {
		var err error
		if c, err = s.commit(ctx, "", nil); err != nil {
			return err
		}
	}
	if _, err := s.git(ctx, nil, nil, "push", "--dry-run", "--porcelain", Remote, c+":"+RefName+"-doctor"); err != nil {
		return fmt.Errorf("can't push %s to %s: %v", RefName, Remote, err)
	}
	return nil
}
