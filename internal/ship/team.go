package ship

import (
	"context"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// PushTeam updates an existing claim. It cannot open a replacement if the
// draft closes during shipping, including between the check and lease push.
func (c *Claim) PushTeam(ctx context.Context, f ClaimForge, save SaveLease) (*forge.MR, error) {
	if save == nil {
		return nil, fmt.Errorf("shipping requires durable lease storage")
	}
	if err := c.Check(ctx, f); err != nil {
		return nil, err
	}
	head, err := c.Git.Head(ctx)
	if err != nil {
		return nil, err
	}
	if head != c.Lease {
		sha, err := c.Git.Push(ctx, c.Lease)
		if err != nil {
			return nil, err
		}
		c.Lease = sha
		if err = save(sha); err != nil {
			return nil, err
		}
	}
	// Recheck the forge claim before updating its title, body or draft state.
	if err := c.Check(ctx, f); err != nil {
		return nil, err
	}
	return c.MR, nil
}

// pushClaim routes every later shipping push through the run's claim rules.
func (e *Engine) pushClaim(ctx context.Context, claim *Claim) (*forge.MR, error) {
	if e.Identity.By != "" {
		return claim.PushTeam(ctx, e.Forge, e.persistPush)
	}
	return claim.Acquire(ctx, e.Forge, e.Request, e.persistPush)
}
