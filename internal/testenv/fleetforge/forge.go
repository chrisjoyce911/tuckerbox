// Package fleetforge is a file-backed forge for multi-process fleet tests.
// It shares no credentials or network access with the user's forge.
package fleetforge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/gofrs/flock"
)

type Entry struct {
	MR         forge.MR
	Branch, CI string
	Checks     int
}
type State struct {
	CI                                          string
	Bare                                        string
	Entries                                     map[int]Entry
	Opens, AcceptedMerges, RefusedMerges, Edits int
}
type Client struct{ Dir string }

func (c Client) update(ctx context.Context, fn func(*State) error) error {
	if err := os.MkdirAll(c.Dir, 0755); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(c.Dir, "lock"))
	ok, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("forge lock unavailable")
	}
	defer lock.Unlock()
	st := State{Entries: map[int]Entry{}}
	p := filepath.Join(c.Dir, "state.json")
	data, err := os.ReadFile(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(data, &st); err != nil {
			return err
		}
	}
	result := fn(&st)
	data, err = json.Marshal(st)
	if err != nil {
		return err
	}
	if err = os.WriteFile(p, data, 0600); err != nil {
		return err
	}
	return result
}
func (c Client) Init(ctx context.Context, bare string) error {
	return c.update(ctx, func(s *State) error { s.Bare = bare; return nil })
}
func (c Client) Snapshot(ctx context.Context) (State, error) {
	var st State
	err := c.update(ctx, func(s *State) error { st = *s; return nil })
	return st, err
}
func (c Client) SetCI(ctx context.Context, state string) error {
	return c.update(ctx, func(s *State) error {
		s.CI = state
		for n, e := range s.Entries {
			e.CI = state
			s.Entries[n] = e
		}
		return nil
	})
}
func head(ctx context.Context, s *State, branch string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", s.Bare, "rev-parse", "--verify", "refs/heads/"+branch).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("reading fake forge head: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}
func current(ctx context.Context, s *State, e Entry) (*forge.MR, error) {
	m := e.MR
	var err error
	m.Head, err = head(ctx, s, e.Branch)
	m.Mergeable = true
	m.MergeStatus = "clean"
	return &m, err
}
func (c Client) Access(context.Context) (forge.Access, error) {
	return forge.Access{Read: true, Push: true, DefaultBranch: "main"}, nil
}
func (c Client) FindMR(ctx context.Context, branch string) (*forge.MR, error) {
	var m *forge.MR
	err := c.update(ctx, func(s *State) error {
		for _, e := range s.Entries {
			if e.Branch == branch && e.MR.State == "open" {
				var err error
				m, err = current(ctx, s, e)
				return err
			}
		}
		return nil
	})
	return m, err
}
func (c Client) FindMergedMR(ctx context.Context, branch string) (*forge.MR, error) {
	var m *forge.MR
	err := c.update(ctx, func(s *State) error {
		for _, e := range s.Entries {
			if e.Branch == branch && e.MR.State == "merged" {
				v := e.MR
				m = &v
				break
			}
		}
		return nil
	})
	return m, err
}
func (c Client) OpenMR(ctx context.Context, r forge.OpenRequest) (*forge.MR, error) {
	var m *forge.MR
	err := c.update(ctx, func(s *State) error {
		for _, e := range s.Entries {
			if e.Branch == r.Branch && e.MR.State == "open" {
				return &forge.APIError{Status: 422, Message: "already open"}
			}
		}
		s.Opens++
		ci := s.CI
		if ci == "" {
			ci = "running"
		}
		e := Entry{Branch: r.Branch, CI: ci, MR: forge.MR{Number: s.Opens, Title: r.Title, Body: r.Body, Base: r.Base, State: "open", URL: fmt.Sprintf("https://forge.example/mr/%d", s.Opens)}}
		s.Entries[e.MR.Number] = e
		var err error
		m, err = current(ctx, s, e)
		return err
	})
	return m, err
}
func (c Client) UpdateMR(ctx context.Context, n int, r forge.OpenRequest) (*forge.MR, error) {
	var m *forge.MR
	err := c.update(ctx, func(s *State) error {
		e, ok := s.Entries[n]
		if !ok {
			return fmt.Errorf("missing MR%d", n)
		}
		e.MR.Body = r.Body
		e.MR.Title = r.Title
		s.Entries[n] = e
		s.Edits++
		var err error
		m, err = current(ctx, s, e)
		return err
	})
	return m, err
}
func (c Client) GetMR(ctx context.Context, n int) (*forge.MR, error) {
	var m *forge.MR
	err := c.update(ctx, func(s *State) error {
		e, ok := s.Entries[n]
		if !ok {
			return fmt.Errorf("missing MR%d", n)
		}
		var err error
		m, err = current(ctx, s, e)
		return err
	})
	return m, err
}
func (c Client) Checks(ctx context.Context, sha, _ string) (forge.Checks, error) {
	var checks forge.Checks
	err := c.update(ctx, func(s *State) error {
		for n, e := range s.Entries {
			tip, err := head(ctx, s, e.Branch)
			if err != nil {
				return err
			}
			if tip == sha {
				e.Checks++
				s.Entries[n] = e
				checks.Jobs = []forge.Job{{ID: "test", Name: "test", State: e.CI}}
				return nil
			}
		}
		return fmt.Errorf("unknown checked head %s", sha)
	})
	return checks, err
}
func (c Client) Threads(context.Context, int) ([]forge.Thread, error) { return nil, nil }
func (c Client) Log(context.Context, forge.Job) (forge.Log, error) {
	return forge.Log{Text: "fake test log"}, nil
}
func (c Client) Reply(context.Context, int, string, string) error { return nil }
func (c Client) Merge(ctx context.Context, n int, sha, _ string) error {
	return c.update(ctx, func(s *State) error {
		e, ok := s.Entries[n]
		if !ok {
			return fmt.Errorf("missing MR%d", n)
		}
		tip, err := head(ctx, s, e.Branch)
		if err != nil {
			return err
		}
		if sha == "" || tip != sha {
			s.RefusedMerges++
			return &forge.APIError{Status: 409, Message: "pinned head changed"}
		}
		if e.CI != "green" {
			return fmt.Errorf("CI is not green")
		}
		s.AcceptedMerges++
		e.MR.State = "merged"
		s.Entries[n] = e
		return nil
	})
}
