package fleet

import "context"

// Fence accompanies every shared shipping effect with durable ownership.
type Fence struct {
	Store Store
	Claim Claim
}

func (f *Fence) Renew(ctx context.Context) error { return f.Store.Renew(ctx, f.Claim) }
func (f *Fence) Push(ctx context.Context, branch, lease, head string) error {
	return f.Store.Push(ctx, f.Claim, branch, lease, head)
}
