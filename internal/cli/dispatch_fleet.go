package cli

import (
	"context"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/dispatch"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
)

// dispatchClaims fetches the fleet ref from remote and reads its claims, by
// change, each with the state a reader works out (expired: active and not
// renewed within lease, allowing for clock skew). A remote without the
// ref has no claims.
func dispatchClaims(ctx context.Context, root, remote string, lease time.Duration, now time.Time) (map[string]dispatch.Claim, error) {
	s := fleet.Store{Dir: root, Remote: remote, Lease: lease, Now: func() time.Time { return now }}
	cs, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]dispatch.Claim, len(cs))
	for id, c := range cs {
		out[id] = dispatch.Claim{Machine: c.Machine, State: c.Effective(lease, now)}
	}
	return out, nil
}
