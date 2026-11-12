package ship

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/chrisjoyce911/tuckerbox/internal/agent"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type Feedback struct {
	Head        string          `json:"head,omitempty"`
	FixedCommit string          `json:"fixed_commit,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Step        string          `json:"step"`
	Findings    []agent.Finding `json:"findings"`
	Threads     []forge.Thread  `json:"threads,omitempty"`
}

var terminalCodes = regexp.MustCompile("\\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\\a]*(?:\\a|\\x1b\\\\))")

const jobLogLimit = 32 << 10
const roundLogLimit = 128 << 10

func TrimLog(raw string, mask func(string) string) string {
	raw = terminalCodes.ReplaceAllString(raw, "")
	if mask != nil {
		raw = mask(raw)
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(raw, "\r\n", "\n"), "\n"), "\n")
	omitted := max(0, len(lines)-200)
	if omitted > 0 {
		lines = lines[omitted:]
	}
	tail := strings.Join(lines, "\n") + "\n"
	prefix := ""
	if omitted > 0 {
		prefix = fmt.Sprintf("[omitted %d lines]\n", omitted)
	}
	if len(prefix)+len(tail) > jobLogLimit {
		prefix = fmt.Sprintf("[omitted %d lines; tail limited to 32 KiB]\n", omitted)
		tail = tail[len(tail)-(jobLogLimit-len(prefix)):]
		for !utf8.ValidString(tail) {
			tail = tail[1:]
		}
	}
	return prefix + tail
}
func (e *Engine) ciFeedback(ctx context.Context, checks forge.Checks) (Feedback, error) {
	feedback := Feedback{Step: e.Config.Shipping.FixCI}
	remaining := roundLogLimit
	for _, job := range checks.Jobs {
		if job.State != "red" {
			continue
		}
		detail := job.URL
		if remaining > 0 {
			raw, err := e.Forge.Log(ctx, job)
			if err != nil {
				return feedback, err
			}
			text := TrimLog(raw, e.text)
			if len(text) > remaining {
				text = text[len(text)-remaining:]
				for !utf8.ValidString(text) {
					text = text[1:]
				}
			}
			if e.LogDir == "" {
				return feedback, fmt.Errorf("shipping log directory is not configured")
			}
			name := fmt.Sprintf("%x.log", sha256.Sum256([]byte(job.ID+"\x00"+job.Name)))
			path := filepath.Join(e.LogDir, "ship", fmt.Sprint(e.Record.ShipRound+1), name)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return feedback, err
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				return feedback, err
			}
			remaining -= len(text)
			detail += "\nSaved log: " + path
		} else {
			detail += "\nRound log limit reached."
		}
		feedback.Findings = append(feedback.Findings, agent.Finding{Severity: "high", File: job.Name, Detail: e.text(detail)})
	}
	return feedback, nil
}
func (e *Engine) reviewFeedback(threads []forge.Thread) Feedback {
	feedback := Feedback{Step: e.Config.Shipping.FixReview}
	for _, thread := range threads {
		if !EligibleThread(thread, e.Config.Shipping.ReviewFrom) {
			continue
		}
		// Our automatic replies are remembered by their exact body, while every
		// other new comment (including one before that reply) remains actionable.
		latest := ""
		var text strings.Builder
		for _, comment := range thread.Comments {
			if e.ownReply(thread.ID, comment.Text) {
				continue
			}
			latest = comment.ID
			fmt.Fprintf(&text, "%s: %s\n%s\n", comment.Author, comment.Text, comment.URL)
		}
		if latest == "" || e.Record.ThreadSeen[thread.ID] == latest {
			continue
		}
		feedback.Threads = append(feedback.Threads, thread)
		feedback.Findings = append(feedback.Findings, agent.Finding{Severity: "high", File: thread.File, Detail: e.text(fmt.Sprintf("Line %d\n%s", thread.Line, text.String()))})
	}
	return feedback
}
func (e *Engine) fix(ctx context.Context, claim *Claim, feedback Feedback) Outcome {
	r := e.Record
	if feedback.Step == "" {
		reason := "review findings need a fix step"
		if r.CI.State == "red" {
			reason = "CI failed"
		}
		return e.blocked(fmt.Errorf("%s", reason))
	}
	if e.Fix == nil {
		return e.blocked(fmt.Errorf("fix step runner is not configured"))
	}
	limit := e.Config.Shipping.MaxRounds
	if limit <= 0 {
		limit = 3
	}
	if r.ShipFixes >= limit {
		return e.blocked(fmt.Errorf("ship limit reached"))
	}
	feedback.Head = r.LastPush
	r.Pending = &feedback
	r.ShipFixes++
	r.ShipRound++
	if r.ThreadSeen == nil {
		r.ThreadSeen = map[string]string{}
	}
	if r.ThreadReplies == nil {
		r.ThreadReplies = map[string][]string{}
	}
	for _, thread := range feedback.Threads {
		for _, comment := range thread.Comments {
			if !e.ownReply(thread.ID, comment.Text) {
				r.ThreadSeen[thread.ID] = comment.ID
			}
		}
	}
	if err := e.save(); err != nil {
		return e.blocked(err)
	}
	if len(feedback.Threads) > 0 {
		e.emit(Event{Kind: "review_findings", Number: len(feedback.Threads)})
	}
	before, err := e.Git.Head(ctx)
	if err != nil {
		return e.blocked(err)
	}
	result, err := e.Fix(ctx, feedback.Step, feedback.Findings)
	if ctx.Err() != nil {
		return Outcome{"stopped", ctx.Err().Error()}
	}
	if err != nil {
		return e.blocked(err)
	}
	if result == nil {
		return e.blocked(fmt.Errorf("fix step returned no result"))
	}
	if result.Status != agent.Done {
		state := "blocked"
		if result.Status == agent.Failed {
			state = "failed"
		}
		return Outcome{state, e.text(result.Summary)}
	}
	if _, err := e.Commit(ctx, e.Identity.Change+": fix forge findings"); err != nil {
		return e.blocked(err)
	}
	after, err := e.Git.Head(ctx)
	if err != nil {
		return e.blocked(err)
	}
	if before == after {
		return e.blocked(fmt.Errorf("fix changed nothing"))
	}
	current, err := e.Forge.GetMR(ctx, r.MR.Number)
	if err != nil {
		return e.blocked(err)
	}
	if current.State == "merged" {
		return e.merged(ctx, r.LastPush)
	}
	if current.State == "closed" {
		return e.blocked(fmt.Errorf("merge request closed"))
	}
	if err := claim.Check(ctx, e.Forge); err != nil {
		return e.blocked(err)
	}
	if _, err := claim.Acquire(ctx, e.Forge, e.Request, e.persistPush); err != nil {
		return e.blocked(err)
	}
	feedback.FixedCommit = r.LastPush
	feedback.Summary = e.text(result.Summary)
	r.Pending = &feedback
	if err := e.save(); err != nil {
		return e.blocked(err)
	}
	return e.replyFeedback(ctx, claim, feedback)
}
func (e *Engine) replyFeedback(ctx context.Context, claim *Claim, feedback Feedback) Outcome {
	r := e.Record
	reply := e.text(fmt.Sprintf("Fixed in %s: %s", feedback.FixedCommit, feedback.Summary))
	for _, thread := range feedback.Threads {
		if e.ownReply(thread.ID, reply) {
			continue
		}
		if err := e.retryAction(ctx, func() error {
			if err := claim.Check(ctx, e.Forge); err != nil {
				return err
			}
			threads, err := e.Forge.Threads(ctx, r.MR.Number)
			if err != nil {
				return err
			}
			for _, current := range threads {
				if current.ID == thread.ID {
					for _, comment := range current.Comments {
						if comment.Text == reply {
							return nil
						}
					}
				}
			}
			return e.Forge.Reply(ctx, r.MR.Number, thread.ID, reply)
		}); err != nil {
			return e.blocked(err)
		}

		if r.ThreadReplies == nil {
			r.ThreadReplies = map[string][]string{}
		}
		r.ThreadReplies[thread.ID] = append(r.ThreadReplies[thread.ID], reply)
		if err := e.save(); err != nil {
			return e.blocked(err)
		}
	}
	if len(feedback.Threads) > 0 {
		e.emit(Event{Kind: "review_replied", Commit: feedback.FixedCommit, Number: len(feedback.Threads)})
	}
	r.Pending = nil
	if err := e.save(); err != nil {
		return e.blocked(err)
	}
	return Outcome{}
}
func (e *Engine) ownReply(thread, text string) bool {
	for _, reply := range e.Record.ThreadReplies[thread] {
		if reply == text {
			return true
		}
	}
	return false
}
