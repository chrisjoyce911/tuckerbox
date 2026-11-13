// Package team implements protected-base approval and forge claim reads.
package team

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"github.com/gofrs/flock"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Claim struct {
	Change, Login, Machine, Run string
	MR                          int
	URL                         string
}
type Snapshot struct {
	Claims      map[string]Claim
	FetchedAt   time.Time
	Stale       bool
	Err         string
	AttemptedAt time.Time
}

func BoardClaims(ctx context.Context, root string, cfg *config.Config, now time.Time) (Snapshot, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return Snapshot{}, err
	}
	path := filepath.Join(strings.TrimSpace(string(out)), "tuckerbox", "team-claims.json")
	unlock, err := lockCache(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	// Check before connecting, so a cache hit doesn't ask credential helpers.
	kept := read(path)
	if !kept.AttemptedAt.IsZero() && now.Sub(kept.AttemptedAt) < time.Minute {
		return kept, nil
	}
	client, mask, err := ship.Connect(ctx, cfg, nil)
	if err != nil {
		if mask != nil {
			err = fmt.Errorf("%s", mask(err.Error()))
		}
		return failed(path, kept, now, err)
	}
	tc, ok := client.(forge.TeamClient)
	if !ok {
		return failed(path, kept, now, fmt.Errorf("forge does not support team claims"))
	}
	return claims(ctx, tc, path, now)
}
func read(path string) Snapshot {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Claims == nil {
		s.Claims = map[string]Claim{}
	}
	return s
}
func write(path string, s Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".team-claims-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func failed(path string, s Snapshot, now time.Time, err error) (Snapshot, error) {
	s.Stale = true
	s.Err = err.Error()
	s.AttemptedAt = now
	if werr := write(path, s); werr != nil {
		return s, werr
	}
	return s, nil
}

// Claims limits forge reads, including failed attempts, to one per minute.
func Claims(ctx context.Context, c forge.TeamClient, path string, now time.Time) (Snapshot, error) {
	unlock, err := lockCache(ctx, path)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	return claims(ctx, c, path, now)
}
func lockCache(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f := flock.New(path + ".lock")
	ok, err := f.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("team claims cache lock unavailable")
	}
	return func() { _ = f.Unlock() }, nil
}
func claims(ctx context.Context, c forge.TeamClient, path string, now time.Time) (Snapshot, error) {
	s := read(path)
	if !s.AttemptedAt.IsZero() && now.Sub(s.AttemptedAt) < time.Minute {
		return s, nil
	}
	list, err := c.ListOpenMRs(ctx)
	if err != nil {
		return failed(path, s, now, err)
	}
	s = Snapshot{Claims: map[string]Claim{}, FetchedAt: now, AttemptedAt: now}
	for _, mr := range list {
		id, ok := ship.ParseMarker(mr.Body)
		if !ok || id.By == "" || mr.Author == "" {
			continue
		}
		s.Claims[id.Change] = Claim{Change: id.Change, Login: mr.Author, Machine: id.Machine, Run: id.Run, MR: mr.Number, URL: mr.URL}
	}
	return s, write(path, s)
}
