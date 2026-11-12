package ship

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type snapshot struct {
	MR      *forge.MR
	Checks  forge.Checks
	Threads []forge.Thread
}

func (e *Engine) poll(ctx context.Context, claim *Claim) (snapshot, error) {
	s := snapshot{}
	var err error
	s.MR, err = e.Forge.GetMR(ctx, e.Record.MR.Number)
	if err != nil {
		return s, err
	}
	if s.MR.State != "open" {
		return s, nil
	}
	if err := claim.owner(s.MR); err != nil {
		return s, err
	}
	if s.MR.Head != "" && s.MR.Head != e.Record.LastPush {
		return s, fmt.Errorf("branch changed on the forge")
	}
	if s.MR.Head == "" {
		if err := claim.Check(ctx, e.Forge); err != nil {
			return s, err
		}
	}
	if e.Config.Ship == config.ShipMerge && s.MR.Behind {
		return s, nil
	}
	s.Checks, err = e.Forge.Checks(ctx, e.Record.LastPush, e.Request.Base)
	if err != nil {
		return s, err
	}
	s.Threads, err = e.Forge.Threads(ctx, s.MR.Number)
	return s, err
}
func (e *Engine) retryDelay(err error, interval time.Duration) (time.Duration, bool) {
	var api *forge.APIError
	if errors.As(err, &api) {
		if api.Status == 429 {
			delay := interval
			if api.RetryAt.After(e.now()) {
				delay = api.RetryAt.Sub(e.now())
			}
			return delay, true
		}
		if api.Status < 500 {
			return 0, false
		}
	} else {
		var request *forge.RequestError
		if !errors.As(err, &request) {
			return 0, false
		}
	}
	if e.Jitter != nil {
		return e.Jitter(interval), true
	}
	return time.Duration(rand.Int64N(int64(interval) + 1)), true
}
func (e *Engine) watch(ctx context.Context, claim *Claim) Outcome {
	r := e.Record
	interval := 30 * time.Second
	lastChange := e.now()
	fingerprint := ""
	if r.Pending != nil {
		var out Outcome
		switch {
		case r.Pending.FixedCommit != "":
			out = e.replyFeedback(ctx, claim, *r.Pending)
		case r.Pending.Head != r.LastPush:
			r.Pending = nil
			if err := e.save(); err != nil {
				return e.blocked(err)
			}
		default:
			out = e.fix(ctx, claim, *r.Pending)
		}
		if out.State != "" {
			return out
		}
	}
	for {
		if ctx.Err() != nil {
			return Outcome{"stopped", ctx.Err().Error()}
		}
		snap, err := e.poll(ctx, claim)
		if err != nil {
			delay, retry := e.retryDelay(err, interval)
			if !retry {
				return e.blocked(err)
			}
			if e.now().Sub(lastChange) >= e.waitTimeout() {
				return e.blocked(fmt.Errorf("waiting for forge timed out"))
			}
			if err := e.sleep(ctx, delay); err != nil {
				return Outcome{"stopped", err.Error()}
			}
			interval = min(interval*2, 5*time.Minute)
			continue
		}
		mr, checks, threads := snap.MR, snap.Checks, snap.Threads
		r.MR = info(mr)
		switch mr.State {
		case "closed":
			return e.blocked(fmt.Errorf("merge request closed"))
		case "merged":
			return e.merged(ctx, r.LastPush)
		}
		if e.Config.Ship == config.ShipMerge && mr.Behind {
			before := r.LastPush
			sha, err := e.Git.UpdateBase(ctx)
			if err != nil {
				return e.blocked(err)
			}
			if sha == before {
				return e.blocked(fmt.Errorf("forge requires a base update but merge changed nothing"))
			}
			if _, err := claim.Acquire(ctx, e.Forge, e.Request, e.persistPush); err != nil {
				return e.blocked(err)
			}
			lastChange = e.now()
			interval = 30 * time.Second
			continue
		}
		ci := CIState(checks, r.PushedAt, e.now())
		oldCI := ""
		if r.CI != nil {
			oldCI = r.CI.State
		}
		r.CI = &CIInfo{State: ci, Commit: r.LastPush}
		if oldCI != ci && ci != "pending" {
			e.emit(Event{Kind: "ci_finished", Commit: r.LastPush, CI: ci})
		}
		if err := e.save(); err != nil {
			return e.blocked(err)
		}
		feedback := e.reviewFeedback(threads)
		if ci == "red" {
			if e.Config.Shipping.FixCI == "" {
				return e.blocked(fmt.Errorf("CI failed"))
			}
			feedback, err = e.ciFeedback(ctx, checks)
			if err != nil {
				if delay, retry := e.retryDelay(err, interval); retry {
					if e.now().Sub(lastChange) >= e.waitTimeout() {
						return e.blocked(fmt.Errorf("waiting for forge timed out"))
					}
					if err := e.sleep(ctx, delay); err != nil {
						return Outcome{"stopped", err.Error()}
					}
					interval = min(interval*2, 5*time.Minute)
					continue
				}
				return e.blocked(err)
			}
		}
		if len(feedback.Findings) > 0 {
			if out := e.fix(ctx, claim, feedback); out.State != "" {
				return out
			}
			lastChange = e.now()
			interval = 30 * time.Second
			fingerprint = ""
			continue
		}
		switch {
		case e.Config.Ship == config.ShipMR && (ci == "green" || ci == "none"):
			return Outcome{State: "ready"}
		case e.Config.Ship == config.ShipMerge && ci == "none":
			return e.blocked(fmt.Errorf("no checks reported"))
		case ci == "green" && mr.Mergeable && e.Config.Ship == config.ShipMerge:
			if err := claim.Check(ctx, e.Forge); err != nil {
				return e.blocked(err)
			}
			if err := e.Forge.Merge(ctx, mr.Number, r.LastPush, e.Config.Shipping.MergeMethod); err != nil {
				if api, ok := err.(*forge.APIError); ok && (api.Status == 409 || api.Status == 422) {
					return e.blocked(fmt.Errorf("branch changed on the forge"))
				}
				return e.blocked(err)
			}
			return e.merged(ctx, r.LastPush)
		}
		data, _ := json.Marshal(snap)
		next := fmt.Sprintf("%x", sha256.Sum256(data))
		if fingerprint != next {
			lastChange = e.now()
			interval = 30 * time.Second
			fingerprint = next
		} else {
			interval = min(interval*2, 5*time.Minute)
		}
		if e.now().Sub(lastChange) >= e.waitTimeout() {
			reason := "waiting for CI timed out"
			if ci == "green" {
				reason = "waiting for approval timed out"
			}
			return e.blocked(fmt.Errorf("%s", reason))
		}
		if err := e.sleep(ctx, interval); err != nil {
			return Outcome{"stopped", err.Error()}
		}
	}
}

// retryAction is for recoverable API actions whose progress is persisted (for
// example, a thread reply after its fix push). It never reruns the fix agent.
func (e *Engine) retryAction(ctx context.Context, action func() error) error {
	started := e.now()
	interval := 30 * time.Second
	for {
		err := action()
		if err == nil {
			return nil
		}
		delay, retry := e.retryDelay(err, interval)
		if !retry {
			return err
		}
		if e.now().Sub(started) >= e.waitTimeout() {
			return fmt.Errorf("waiting for forge timed out")
		}
		if err := e.sleep(ctx, delay); err != nil {
			return err
		}
		interval = min(interval*2, 5*time.Minute)
	}
}
