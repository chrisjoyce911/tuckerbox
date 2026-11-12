package ship

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type MRInfo struct {
	Link   string `json:"link"`
	Number int    `json:"number"`
	State  string `json:"state"`
}
type CIInfo struct {
	State  string `json:"state"`
	Commit string `json:"commit"`
}

// Record is persisted with run state by Save, while the caller holds the change lock.
type Record struct {
	ShipFixes       int                 `json:"ship_fixes,omitempty"`
	ThreadSeen      map[string]string   `json:"thread_seen,omitempty"`
	ThreadReplies   map[string][]string `json:"thread_replies,omitempty"`
	Pending         *Feedback           `json:"pending_feedback,omitempty"`
	ShippingStarted bool                `json:"shipping_started,omitempty"`
	LastPush        string              `json:"last_push,omitempty"`
	PushedAt        time.Time           `json:"pushed_at,omitempty"`
	ArchiveDone     bool                `json:"archive_done,omitempty"`
	ArchivePath     string              `json:"archive_path,omitempty"`
	MR              *MRInfo             `json:"mr,omitempty"`
	CI              *CIInfo             `json:"ci,omitempty"`
	ShipRound       int                 `json:"ship_round,omitempty"`
}
type Outcome struct{ State, Reason string }
type Event struct {
	CI                                 string
	Kind, Commit, Branch, Link, Method string
	Number, Round                      int
}
type Engine struct {
	Fix      func(context.Context, string, []agent.Finding) (*agent.Result, error)
	LogDir   string
	Jitter   func(time.Duration) time.Duration
	Config   *config.Config
	Git      Git
	Identity Identity
	Forge    forge.Client
	Record   *Record
	Request  forge.OpenRequest
	Save     func() error
	Event    func(Event)
	Now      func() time.Time
	Sleep    func(context.Context, time.Duration) error
	// Archive and Commit are hooks for the repository's OpenSpec and git commands.
	Archive func(context.Context, string) (string, error)
	Commit  func(context.Context, string) (bool, error)
	// ReleaseSlot frees the workflow slot once shipping begins waiting.
	ReleaseSlot func() error
	Mask        func(string) string
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
func (e *Engine) sleep(ctx context.Context, d time.Duration) error {
	if e.Sleep != nil {
		return e.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (e *Engine) text(s string) string {
	if e.Mask != nil {
		return e.Mask(s)
	}
	return s
}
func (e *Engine) emit(ev Event) {
	if e.Event != nil {
		e.Event(ev)
	}
}
func (e *Engine) save() error {
	if e.Save == nil {
		return fmt.Errorf("shipping requires durable run state")
	}
	return e.Save()
}
func (e *Engine) blocked(err error) Outcome {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Outcome{"stopped", err.Error()}
	}
	return Outcome{"blocked", e.text(err.Error())}
}
func (e *Engine) persistPush(sha string) error {
	e.Record.LastPush = sha
	e.Record.PushedAt = e.now()
	e.Record.CI = &CIInfo{State: "pending", Commit: sha}
	if err := e.save(); err != nil {
		return err
	}
	e.emit(Event{Kind: "pushed", Commit: sha, Branch: e.Git.Branch, Round: e.Record.ShipRound})
	return nil
}
func info(m *forge.MR) *MRInfo { return &MRInfo{Link: m.URL, Number: m.Number, State: m.State} }

// Run executes shipping only after the caller has applied task completeness
// rules. Incomplete scopes must never call it, even with archive disabled.
func (e *Engine) Run(ctx context.Context, resume bool) Outcome {
	if e.Config == nil || e.Record == nil || e.Forge == nil || e.Commit == nil {
		return e.blocked(fmt.Errorf("shipping is not configured"))
	}
	r := e.Record
	if resume {
		r.ShipFixes = 0
		r.ShipRound = 1
	}
	if e.Request.Base == "" {
		e.Request.Base = strings.TrimPrefix(e.Git.Base, e.Git.remote()+"/")
	}
	if r.MR != nil {
		current, err := e.Forge.GetMR(ctx, r.MR.Number)
		if err != nil {
			return e.blocked(err)
		}
		if current.State == "closed" {
			return e.blocked(fmt.Errorf("merge request closed"))
		}
		if current.State == "merged" {
			return e.merged(ctx, r.LastPush)
		}
	}
	if !r.ShippingStarted {
		r.ShippingStarted = true
		r.ShipRound = 1
		if err := e.save(); err != nil {
			return e.blocked(err)
		}
	}
	if !r.ArchiveDone {
		if e.Config.Shipping.Archive {
			archive := e.Archive
			if archive == nil {
				archive = e.archive
			}
			folder, err := archive(ctx, e.Identity.Change)
			if err != nil {
				return e.blocked(err)
			}
			r.ArchivePath = folder
			if _, err := e.Commit(ctx, "Archive "+e.Identity.Change); err != nil {
				return e.blocked(err)
			}
			sha, err := e.Git.Head(ctx)
			if err != nil {
				return e.blocked(err)
			}
			e.emit(Event{Kind: "archived", Commit: sha})
		}
		r.ArchiveDone = true
		if err := e.save(); err != nil {
			return e.blocked(err)
		}
	}
	if !resume {
		if _, err := e.Commit(ctx, e.Identity.Change+": work from tuckerbox run"); err != nil {
			return e.blocked(err)
		}
	}
	if e.Request.Title == "" {
		request, err := ProposalRequest(e.Git.Worktree, e.Identity.Change, e.Config.ForgeTarget, e.Git.Branch, strings.TrimPrefix(e.Git.Base, e.Git.remote()+"/"), e.Config.Shipping.Labels, e.Identity)
		if err != nil {
			return e.blocked(err)
		}
		e.Request = request
	}
	count, err := e.Git.run(ctx, "rev-list", "--count", e.Git.Base+"..refs/heads/"+e.Git.Branch)
	if err != nil {
		return e.blocked(err)
	}
	n, err := strconv.Atoi(count)
	if err != nil || n == 0 {
		return e.blocked(fmt.Errorf("branch has no commits beyond the base"))
	}
	claim := &Claim{Identity: e.Identity, Git: e.Git, Lease: r.LastPush}
	hadMR := r.MR != nil
	var mr *forge.MR
	if resume {
		mr, err = claim.Resume(ctx, e.Forge, e.Request, func(sha string) error {
			if !claim.adopting {
				return e.persistPush(sha)
			}
			r.LastPush = sha
			r.PushedAt = e.now()
			r.CI = &CIInfo{State: "pending", Commit: sha}
			return e.save()
		})
	} else {
		mr, err = claim.Acquire(ctx, e.Forge, e.Request, e.persistPush)
	}
	if err != nil {
		return e.blocked(err)
	}
	r.MR = info(mr)
	if err := e.save(); err != nil {
		return e.blocked(err)
	}
	if !hadMR {
		e.emit(Event{Kind: "mr_opened", Link: mr.URL, Number: mr.Number})
	}
	if e.ReleaseSlot != nil {
		if err := e.ReleaseSlot(); err != nil {
			return e.blocked(err)
		}
	}
	return e.watch(ctx, claim)
}

func (e *Engine) waitTimeout() time.Duration {
	if e.Config.Shipping.WaitTimeout <= 0 {
		return 2 * time.Hour
	}
	return e.Config.Shipping.WaitTimeout
}
func (e *Engine) merged(ctx context.Context, head string) Outcome {
	e.Record.MR.State = "merged"
	if err := e.save(); err != nil {
		return e.blocked(err)
	}
	e.emit(Event{Kind: "merged", Commit: head, Method: e.Config.Shipping.MergeMethod})
	deleted, err := e.Git.DeleteMerged(ctx, head)
	if err == nil && deleted {
		e.emit(Event{Kind: "branch_deleted", Branch: e.Git.Branch})
	}
	// A moved branch or failed cleanup never reverses a confirmed merge.
	return Outcome{State: "merged"}
}
func (e *Engine) archive(ctx context.Context, change string) (string, error) {
	for _, args := range [][]string{{"archive", change, "--yes"}, {"validate", "--all", "--strict"}} {
		cmd := exec.CommandContext(ctx, "openspec", args...)
		cmd.Dir = e.Git.Worktree
		out, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("openspec %s: %s", strings.Join(args, " "), e.text(string(out)))
		}
	}
	folder, err := changeFolder(e.Git.Worktree, change)
	if err != nil {
		return "", err
	}
	return folder, nil
}
func CIState(checks forge.Checks, pushed, now time.Time) string {
	names := map[string]bool{}
	pending := false
	for _, j := range checks.Jobs {
		names[j.Name] = true
		if j.State == "red" {
			return "red"
		}
		if j.State != "green" {
			pending = true
		}
	}
	for _, name := range checks.Required {
		if !names[name] {
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	if len(checks.Jobs) > 0 {
		return "green"
	}
	if now.Sub(pushed) >= 5*time.Minute {
		return "none"
	}
	return "pending"
}
func EligibleThread(t forge.Thread, allowed []string) bool {
	if t.Resolved || len(t.Comments) == 0 {
		return false
	}
	author := t.Comments[0]
	if author.Writer {
		return true
	}
	for _, name := range allowed {
		if name == author.Author {
			return true
		}
	}
	return false
}
