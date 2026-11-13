// Package fleet implements the compare-and-swap protocol in formal/fleet.tla.
// Clocks control takeover eligibility; atomic ref leases provide safety.
package fleet

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

const RefName = "refs/tuckerbox/fleet"
const (
	Active = "active"
	Parked = "parked"
	Ready  = "ready"
)

type Claim struct {
	Change    string    `toml:"-" json:"change"`
	Machine   string    `toml:"machine" json:"machine"`
	Run       string    `toml:"run" json:"run"`
	Epoch     uint64    `toml:"epoch" json:"epoch"`
	State     string    `toml:"state" json:"state"`
	LastPush  string    `toml:"last_push,omitempty" json:"last_push,omitempty"`
	RenewedAt time.Time `toml:"renewed_at" json:"renewed_at"`
}

func (c Claim) Expired(now time.Time, lease time.Duration) bool {
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	return c.State == Active && now.After(c.RenewedAt.Add(lease+time.Minute))
}
func (c Claim) Effective(lease time.Duration, now time.Time) string {
	if c.Expired(now, lease) {
		return "expired"
	}
	return c.State
}
func (c Claim) same(other Claim) bool {
	return c.Machine == other.Machine && c.Run == other.Run && c.Epoch == other.Epoch
}

type Store struct {
	Dir, Remote string
	Lease       time.Duration
	MaxRuns     int
	Now         func() time.Time
	// Base is a fetched base commit. Writes prune claims archived on that base.
	Base       string
	beforePush func()
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s Store) remote() string {
	if s.Remote == "" {
		return "origin"
	}
	return s.Remote
}
func (s Store) git(ctx context.Context, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", s.Dir}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=tuckerbox", "GIT_AUTHOR_EMAIL=tuckerbox@invalid", "GIT_COMMITTER_NAME=tuckerbox", "GIT_COMMITTER_EMAIL=tuckerbox@invalid")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), transport("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// fetch uses a private local ref: simultaneous renewals/readers cannot replace
// the snapshot between fetch and reading it. Never use a tracking ref as a lease.
func (s Store) fetch(ctx context.Context, ref string) (string, error) {
	if strings.HasPrefix(s.remote(), "-") {
		return "", fmt.Errorf("invalid fleet remote")
	}
	out, err := s.git(ctx, "", "ls-remote", "--refs", "--", s.remote(), ref)
	if err != nil || out == "" {
		return "", err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return "", err
	}
	tmp := "refs/tuckerbox/read/" + hex.EncodeToString(nonce[:])
	defer s.git(context.Background(), "", "update-ref", "-d", tmp)
	if _, err = s.git(ctx, "", "fetch", "--quiet", "--no-tags", "--", s.remote(), "+"+ref+":"+tmp); err != nil {
		return "", err
	}
	return s.git(ctx, "", "rev-parse", tmp+"^{commit}")
}

type snapshot struct {
	tip      string
	claims   map[string]Claim
	epochs   map[string]uint64
	archived map[string]bool
	merged   map[string]bool
	baseTip  string
}

func (s Store) read(ctx context.Context) (snapshot, error) {
	tip, err := s.fetch(ctx, RefName)
	if err != nil {
		return snapshot{}, err
	}
	if tip != "" {
		_, _ = s.git(ctx, "", "update-ref", RefName, tip)
	} else {
		_, _ = s.git(ctx, "", "update-ref", "-d", RefName)
	}
	return s.readTip(ctx, tip)
}

func (s Store) ReadLocal(ctx context.Context) (map[string]Claim, error) {
	tip, err := s.git(ctx, "", "rev-parse", "--verify", "--quiet", RefName+"^{commit}")
	if err != nil {
		return map[string]Claim{}, nil
	}
	st, err := s.readTip(ctx, tip)
	return st.claims, err
}
func (s Store) readTip(ctx context.Context, tip string) (snapshot, error) {
	st := snapshot{claims: map[string]Claim{}, epochs: map[string]uint64{}, merged: map[string]bool{}}
	var err error
	st.tip = tip
	if err != nil || st.tip == "" {
		return st, err
	}
	paths, err := s.git(ctx, "", "ls-tree", "-r", "--name-only", st.tip)
	if err != nil {
		return st, err
	}
	for _, p := range strings.Split(paths, "\n") {
		if !strings.HasPrefix(p, "claims/") && !strings.HasPrefix(p, "epochs/") && !strings.HasPrefix(p, "merged/") {
			continue
		}
		data, err := s.git(ctx, "", "show", st.tip+":"+p)
		if err != nil {
			return st, err
		}
		if strings.HasPrefix(p, "merged/") {
			st.merged[strings.TrimPrefix(p, "merged/")] = true
			continue
		}
		if strings.HasPrefix(p, "epochs/") {
			v, err := strconv.ParseUint(data, 10, 64)
			if err != nil {
				return st, err
			}
			st.epochs[strings.TrimPrefix(p, "epochs/")] = v
			continue
		}
		var c Claim
		if _, err = toml.Decode(data, &c); err != nil {
			return st, fmt.Errorf("fleet %s: %w", p, err)
		}
		c.Change = strings.TrimSuffix(strings.TrimPrefix(p, "claims/"), ".toml")
		if !validName(c.Change) || c.Machine == "" || c.Run == "" || c.Epoch == 0 || (c.State != Active && c.State != Parked && c.State != Ready) || c.RenewedAt.IsZero() {
			return st, fmt.Errorf("invalid fleet claim %s", p)
		}
		st.claims[c.Change] = c
		st.epochs[c.Change] = max(st.epochs[c.Change], c.Epoch)
	}
	return st, nil
}
func (s Store) Read(ctx context.Context) (map[string]Claim, error) {
	st, err := s.read(ctx)
	return st.claims, err
}
func validName(n string) bool { return statedir.CheckID("change", n) == nil }

func (s Store) prune(ctx context.Context, st *snapshot) error {
	base := s.Base
	if base == "" {
		out, err := s.git(ctx, "", "ls-remote", "--symref", "--", s.remote(), "HEAD")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.Fields(line)
			if len(parts) == 3 && parts[0] == "ref:" {
				base = parts[1]
				break
			}
		}
	} else {
		base = strings.TrimPrefix(base, "refs/remotes/")
		base = strings.TrimPrefix(base, s.remote()+"/")
		if !strings.HasPrefix(base, "refs/heads/") {
			base = "refs/heads/" + base
		}
	}
	if base == "" {
		return nil
	}
	tip, err := s.fetch(ctx, base)
	if err != nil {
		return err
	}
	if tip == "" {
		return nil
	}
	st.baseTip = tip
	paths, err := s.git(ctx, "", "ls-tree", "--name-only", tip, "openspec/changes/archive/")
	if err != nil {
		return err
	}
	for _, p := range strings.Split(paths, "\n") {
		name := strings.TrimPrefix(p, "openspec/changes/archive/")
		if len(name) > 11 && name[10] == '-' {
			if _, err := time.Parse("2006-01-02", name[:10]); err == nil {
				if st.archived == nil {
					st.archived = map[string]bool{}
				}
				st.archived[name[11:]] = true
				delete(st.claims, name[11:])
			}
		}
	}
	return nil
}

func (s Store) commit(ctx context.Context, st snapshot) (string, error) {
	trees := map[string]string{}
	for _, folder := range []string{"claims", "epochs", "merged"} {
		var names []string
		if folder == "claims" {
			for n := range st.claims {
				names = append(names, n)
			}
		} else if folder == "merged" {
			for n := range st.merged {
				names = append(names, n)
			}
		} else {
			for n := range st.epochs {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		var tree strings.Builder
		for _, n := range names {
			var data string
			file := n
			if folder == "claims" {
				var b bytes.Buffer
				if err := toml.NewEncoder(&b).Encode(st.claims[n]); err != nil {
					return "", err
				}
				data = b.String()
				file += ".toml"
			} else if folder == "merged" {
				data = "merged\n"
			} else {
				data = strconv.FormatUint(st.epochs[n], 10) + "\n"
			}
			blob, err := s.git(ctx, data, "hash-object", "-w", "--stdin")
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&tree, "100644 blob %s\t%s\n", blob, file)
		}
		t, err := s.git(ctx, tree.String(), "mktree")
		if err != nil {
			return "", err
		}
		trees[folder] = t
	}
	root, err := s.git(ctx, "040000 tree "+trees["claims"]+"\tclaims\n040000 tree "+trees["epochs"]+"\tepochs\n040000 tree "+trees["merged"]+"\tmerged\n", "mktree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", root, "-m", "Update fleet claims"}
	if st.tip != "" {
		args = append(args, "-p", st.tip)
	}
	return s.git(ctx, "", args...)
}
func (s Store) owned(st snapshot, c Claim) (Claim, error) {
	cur, ok := st.claims[c.Change]
	if !ok {
		return Claim{}, &OwnershipError{}
	}
	if !cur.same(c) {
		return Claim{}, &OwnershipError{Machine: cur.Machine}
	}
	return cur, nil
}
func (s Store) admit(st snapshot, change string) error {
	var counted []string
	for n, c := range st.claims {
		if n != change && (c.State == Active || c.State == Parked) {
			counted = append(counted, n)
		}
	}
	if s.MaxRuns > 0 && len(counted) >= s.MaxRuns {
		sort.Strings(counted)
		return fmt.Errorf("fleet limit reached: %s", strings.Join(counted, ", "))
	}
	return nil
}

type branchMove struct{ name, old, new string }

// update retries from a fresh snapshot; the decision function must never reuse
// admission or ownership decisions from an earlier attempt.
func (s Store) update(ctx context.Context, decide func(*snapshot) (*branchMove, error)) error {
	for attempt := 0; attempt < 8; attempt++ {
		st, err := s.read(ctx)
		if err != nil {
			return err
		}
		if err = s.prune(ctx, &st); err != nil {
			return err
		}
		move, err := decide(&st)
		if err != nil {
			return err
		}
		commit, err := s.commit(ctx, st)
		if err != nil {
			return err
		}
		args := []string{"push", "--porcelain", "--atomic", "--force-with-lease=" + RefName + ":" + st.tip}
		if move != nil {
			args = append(args, "--force-with-lease=refs/heads/"+move.name+":"+move.old)
		}
		args = append(args, "--", s.remote(), commit+":"+RefName)
		if move != nil {
			args = append(args, move.new+":refs/heads/"+move.name)
		}
		if s.beforePush != nil {
			s.beforePush()
		}
		out, err := s.git(ctx, "", args...)
		if err == nil {
			_, _ = s.git(ctx, "", "update-ref", RefName, commit)
			return nil
		}
		msg := out + err.Error()
		if !strings.Contains(msg, "[rejected]") && !strings.Contains(msg, "stale info") && !strings.Contains(msg, "fetch first") && !strings.Contains(msg, "cannot lock ref") {
			return err
		}
	}
	return transport("fleet refs kept moving; try again")
}

// Acquire claims an unclaimed change or explicitly takes one over. Returning
// BranchTip means takeover fenced an existing branch and shipping must resume.
type Acquired struct {
	Claim     Claim
	BranchTip string
}

func (s Store) Acquire(ctx context.Context, change, machine, run, branch string, takeOver, force bool) (Acquired, error) {
	var result Acquired
	if !validName(change) || machine == "" || run == "" {
		return result, fmt.Errorf("invalid fleet identity")
	}
	if err := s.checkBranch(ctx, branch); err != nil {
		return result, err
	}
	err := s.update(ctx, func(st *snapshot) (*branchMove, error) {
		if st.merged[change] {
			return nil, fmt.Errorf("change %s is already merged", change)
		}
		if st.archived[change] {
			return nil, fmt.Errorf("change %s is already archived on the base branch", change)
		}
		old, exists := st.claims[change]
		if exists {
			if !takeOver {
				return nil, fmt.Errorf("claimed by %s (%s); use --take-over for an expired claim", old.Machine, old.Effective(s.Lease, s.now()))
			}
			if !old.Expired(s.now(), s.Lease) && !(force && old.State == Parked) {
				return nil, fmt.Errorf("claimed by %s (%s); parked claims require --force", old.Machine, old.State)
			}
		} else if takeOver {
			return nil, fmt.Errorf("no claim to take over for %s", change)
		}
		if takeOver && st.baseTip != "" {
			published := old.LastPush
			if published == "" {
				var err error
				published, err = s.fetch(ctx, "refs/heads/"+branch)
				if err != nil {
					return nil, err
				}
			}
			if published != "" {
				if _, err := s.git(ctx, "", "merge-base", "--is-ancestor", published, st.baseTip); err == nil {
					return nil, fmt.Errorf("change %s is already merged", change)
				}
			}
		}
		if err := s.admit(*st, change); err != nil {
			return nil, err
		}
		c := Claim{Change: change, Machine: machine, Run: run, Epoch: st.epochs[change] + 1, State: Active, RenewedAt: s.now().UTC(), LastPush: old.LastPush}
		st.claims[change] = c
		st.epochs[change] = c.Epoch
		result = Acquired{Claim: c}
		if exists {
			move, err := s.moveBranch(ctx, branch, "Take over "+change+" from "+old.Machine)
			if move != nil {
				c.LastPush = move.new
				st.claims[change] = c
				result.Claim = c
				result.BranchTip = move.new
			}
			return move, err
		}
		return nil, nil
	})
	return result, err
}
func (s Store) SetState(ctx context.Context, c Claim, state string) error {
	_, err := s.Transition(ctx, c, state)
	return err
}

func (s Store) Transition(ctx context.Context, c Claim, state string) (Claim, error) {
	var previous Claim
	if state != Active && state != Parked && state != Ready {
		return previous, fmt.Errorf("invalid fleet state")
	}
	err := s.update(ctx, func(st *snapshot) (*branchMove, error) {
		cur, err := s.owned(*st, c)
		if err != nil {
			return nil, err
		}
		if state == Active && cur.State == Ready {
			if err = s.admit(*st, c.Change); err != nil {
				return nil, err
			}
		}
		previous = cur
		cur.State = state
		cur.RenewedAt = s.now().UTC()
		st.claims[c.Change] = cur
		return nil, nil
	})
	return previous, err
}

func (s Store) Renew(ctx context.Context, c Claim) error {
	return s.update(ctx, func(st *snapshot) (*branchMove, error) {
		cur, err := s.owned(*st, c)
		if err != nil {
			return nil, err
		}
		cur.RenewedAt = s.now().UTC()
		st.claims[c.Change] = cur
		return nil, nil
	})
}

// Push renews ownership and updates the branch in the SAME atomic push. Branch
// refusal is terminal; only fleet contention may retry with unchanged branch lease.
func (s Store) Push(ctx context.Context, c Claim, branch, lease, head string) error {
	if err := s.checkBranch(ctx, branch); err != nil {
		return err
	}
	return s.update(ctx, func(st *snapshot) (*branchMove, error) {
		cur, err := s.owned(*st, c)
		if err != nil {
			return nil, err
		}
		tip, err := s.fetch(ctx, "refs/heads/"+branch)
		if err != nil {
			return nil, err
		}
		if tip != lease {
			return nil, fmt.Errorf("branch changed on the forge")
		}
		cur.RenewedAt = s.now().UTC()
		if head != "" {
			cur.LastPush = head
		}
		st.claims[c.Change] = cur
		return &branchMove{branch, lease, head}, nil
	})
}
func (s Store) checkBranch(ctx context.Context, branch string) error {
	if strings.HasPrefix(branch, "-") || branch == "HEAD" {
		return fmt.Errorf("invalid fleet branch")
	}
	if _, err := s.git(ctx, "", "check-ref-format", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("invalid fleet branch: %v", err)
	}
	out, err := s.git(ctx, "", "ls-remote", "--symref", "--", s.remote(), "HEAD")
	if err != nil {
		return err
	}
	if strings.Contains(out, "ref: refs/heads/"+branch+"\tHEAD") {
		return fmt.Errorf("will not move the default branch")
	}
	return nil
}
func (s Store) moveBranch(ctx context.Context, branch, message string) (*branchMove, error) {
	if err := s.checkBranch(ctx, branch); err != nil {
		return nil, err
	}
	old, err := s.fetch(ctx, "refs/heads/"+branch)
	if err != nil || old == "" {
		return nil, err
	}
	tree, err := s.git(ctx, "", "rev-parse", old+"^{tree}")
	if err != nil {
		return nil, err
	}
	next, err := s.git(ctx, "", "commit-tree", tree, "-p", old, "-m", message)
	if err != nil {
		return nil, err
	}
	return &branchMove{branch, old, next}, nil
}

// Release fences even the holder's already-authorized pinned merge by moving
// the branch tip together with removing the claim. A merged cleanup needs no move.
func (s Store) Release(ctx context.Context, change, machine, branch string, force bool) error {
	return s.update(ctx, func(st *snapshot) (*branchMove, error) {
		c, ok := st.claims[change]
		if !ok {
			return nil, &OwnershipError{}
		}
		if c.Machine != machine && !force {
			return nil, fmt.Errorf("claimed by %s; --force removes it", c.Machine)
		}
		move, err := s.moveBranch(ctx, branch, "Release "+change+" from "+c.Machine)
		if err != nil {
			return nil, err
		}
		delete(st.claims, change)
		return move, nil
	})
}
func (s Store) RemoveMerged(ctx context.Context, c Claim) error { return s.RecordMerged(ctx, c.Change) }

// RecordMerged records authoritative forge evidence independently of an old
// holder's identity. A merge may have completed just before a takeover; the
// tombstone closes that race and prevents any subsequent initial claim.
func (s Store) RecordMerged(ctx context.Context, change string) error {
	if !validName(change) {
		return fmt.Errorf("invalid change")
	}
	return s.update(ctx, func(st *snapshot) (*branchMove, error) {
		st.merged[change] = true
		delete(st.claims, change)
		return nil, nil
	})
}

// Abandon rolls back only this exact startup claim, fencing any takeover branch.
func (s Store) Abandon(ctx context.Context, c Claim, branch string) error {
	return s.update(ctx, func(st *snapshot) (*branchMove, error) {
		if _, err := s.owned(*st, c); err != nil {
			return nil, err
		}
		move, err := s.moveBranch(ctx, branch, "Release "+c.Change+" from "+c.Machine)
		if err != nil {
			return nil, err
		}
		delete(st.claims, c.Change)
		return move, nil
	})
}
