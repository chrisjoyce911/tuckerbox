package board

import (
	"context"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/fleet"
	"github.com/chrisjoyce911/tuckerbox/internal/team"
)

// The production claim readers: the fleet ref through fleet.Store, team
// claims through team.BoardClaims (which keeps its answer in the state
// directory and asks the forge at most once a minute).
func init() {
	FleetClaims = openFleet
	TeamClaims = teamClaims
}

func teamClaims(ctx context.Context, root string, cfg *config.Config, at time.Time) (TeamSnapshot, error) {
	s, err := team.BoardClaims(ctx, root, cfg, at)
	out := TeamSnapshot{Claims: make(map[string]TeamClaim, len(s.Claims)), FetchedAt: s.FetchedAt, Stale: s.Stale, Err: s.Err}
	for id, c := range s.Claims {
		out.Claims[id] = TeamClaim{Change: id, Login: c.Login, Machine: c.Machine, Run: c.Run}
	}
	return out, err
}

// openFleet reads the repository's fleet ref (fleet-claims: The fleet ref)
// on the shipping remote, as runs and claim commands do.
func openFleet(root string, cfg *config.Config) FleetSource {
	return fleetRef{fleet.Store{Dir: root, Remote: cfg.Shipping.Remote, Lease: cfg.Fleet.Lease, MaxRuns: cfg.Fleet.MaxRuns}}
}

type fleetRef struct{ s fleet.Store }

func (f fleetRef) Fetch(ctx context.Context) (map[string]FleetRecord, error) {
	cs, err := f.s.Read(ctx)
	return records(cs), err
}

func (f fleetRef) Local(ctx context.Context) (map[string]FleetRecord, error) {
	cs, err := f.s.ReadLocal(ctx)
	return records(cs), err
}

func records(cs map[string]fleet.Claim) map[string]FleetRecord {
	out := make(map[string]FleetRecord, len(cs))
	for id, c := range cs {
		out[id] = FleetRecord{Change: id, Machine: c.Machine, Run: c.Run, State: c.State, Epoch: c.Epoch, RenewedAt: c.RenewedAt}
	}
	return out
}
