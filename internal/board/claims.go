package board

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// modes-board: the repository's mode, and each change's claim, on the
// board. In fleet mode claims come from the fleet ref
// (refs/tuckerbox/fleet), fetched at most once a minute; in team mode from
// the forge's open merge requests with claim markers, read at most once a
// minute and kept in the state directory. The packages that own those
// claims plug in through FleetClaims and TeamClaims, so the board reads
// them and never writes them.

// FleetCount is the fleet limit and the claims counted against it: active
// and parked ones, as the fleet ref's compare-and-swap counts them.
type FleetCount struct {
	MaxRuns int `json:"max_runs"`
	Claims  int `json:"claims"`
}

// Claim is a card's claim. State is active, parked, expired or ready in
// fleet mode, open in team mode. Epoch and RenewedAt are fleet mode's;
// Login, the forge login of the merge request's author, is team mode's.
type Claim struct {
	Machine   string `json:"machine"`
	State     string `json:"state"`
	Epoch     uint64 `json:"epoch,omitempty"`
	RenewedAt string `json:"renewed_at,omitempty"`
	Login     string `json:"login,omitempty"`
	Run       string `json:"run,omitempty"`
}

// ClaimsRead says where the board's claims came from and when: source
// "fleet" (the fleet ref) or "forge" (team mode's merge requests). Stale is
// true when the source couldn't be read and a kept answer, At old, is
// shown; Error says why.
type ClaimsRead struct {
	Source string `json:"source"`
	At     string `json:"at,omitempty"`
	Stale  bool   `json:"stale,omitempty"`
	Error  string `json:"error,omitempty"`
}

// FleetRecord is one claim in the fleet ref. State is what is stored:
// active, parked or ready (expiry is worked out by the reader).
type FleetRecord struct {
	Change, Machine, Run, State string
	Epoch                       uint64
	RenewedAt                   time.Time
}

// FleetSource reads a repository's fleet ref: Fetch fetches it from the
// remote first, Local reads the copy last fetched. Both return no claims,
// not an error, when the ref doesn't exist.
type FleetSource interface {
	Fetch(ctx context.Context) (map[string]FleetRecord, error)
	Local(ctx context.Context) (map[string]FleetRecord, error)
}

// TeamClaim is one open merge request carrying a claim marker.
type TeamClaim struct {
	Change, Login, Machine, Run string
}

// TeamSnapshot is team mode's claims as last read from the forge. Stale is
// true when the forge couldn't be reached and the kept answer, FetchedAt
// old, is returned; Err says why.
type TeamSnapshot struct {
	Claims    map[string]TeamClaim
	FetchedAt time.Time
	Stale     bool
	Err       string
}

var (
	// FleetClaims opens the fleet ref of the repository at root; nil until
	// the fleet package is linked in.
	FleetClaims func(root string, cfg *config.Config) FleetSource
	// TeamClaims reads team mode's claims for the repository at root, at
	// most once a minute, keeping the answer in the state directory; nil
	// until the team package is linked in.
	TeamClaims func(ctx context.Context, root string, cfg *config.Config, now time.Time) (TeamSnapshot, error)
	// now is the board's clock (tests set it).
	now = time.Now
)

// claimsMaxAge is how often a board read may fetch the fleet ref.
const claimsMaxAge = time.Minute

// skew is the clock difference allowed between machines before an active
// claim counts as expired (fleet-claims: Expired claims).
const skew = time.Minute

// fleetState is a stored claim's state as people see it: an active claim
// not renewed within lease (plus skew) is expired.
func fleetState(r FleetRecord, lease time.Duration, at time.Time) string {
	if r.State == "active" && !r.RenewedAt.IsZero() && at.Sub(r.RenewedAt) > lease+skew {
		return "expired"
	}
	return r.State
}

// readClaims puts the repository's mode, its fleet count and each card's
// claim on doc. common is the repository's git common directory, where the
// fetch stamp lives.
func readClaims(ctx context.Context, doc *Document, root, common string, cfg *config.Config) {
	mode := cfg.Mode
	if mode == "" {
		mode = config.ModeSolo
	}
	doc.Mode = string(mode)
	switch mode {
	case config.ModeFleet:
		readFleet(ctx, doc, root, common, cfg)
	case config.ModeTeam:
		readTeam(ctx, doc, root, cfg)
	}
}

func readFleet(ctx context.Context, doc *Document, root, common string, cfg *config.Config) {
	doc.Fleet = &FleetCount{MaxRuns: cfg.Fleet.MaxRuns}
	read := &ClaimsRead{Source: "fleet"}
	doc.ClaimsRead = read
	if FleetClaims == nil {
		read.Stale, read.Error = true, "fleet claims are not available in this build"
		return
	}
	src := FleetClaims(root, cfg)
	stamp := filepath.Join(common, "tuckerbox", "fleet-fetched")
	t := now()
	var records map[string]FleetRecord
	var err error
	fi, statErr := os.Stat(stamp)
	if statErr == nil && t.Sub(fi.ModTime()) < claimsMaxAge && t.Sub(fi.ModTime()) >= 0 {
		records, err = src.Local(ctx)
		read.At = timestamp(fi.ModTime())
	} else {
		records, err = src.Fetch(ctx)
		if err == nil {
			read.At = timestamp(t)
			if os.MkdirAll(filepath.Dir(stamp), 0o755) == nil {
				_ = os.WriteFile(stamp, nil, 0o644)
				_ = os.Chtimes(stamp, t, t)
			}
		} else {
			// The remote can't be reached: show the copy last fetched, and
			// say how old it is.
			read.Stale, read.Error = true, err.Error()
			if statErr == nil {
				read.At = timestamp(fi.ModTime())
			}
			records, err = src.Local(ctx)
		}
	}
	if err != nil {
		read.Stale = true
		if read.Error == "" {
			read.Error = err.Error()
		}
		return
	}
	lease := cfg.Fleet.Lease
	if lease <= 0 {
		lease = config.DefaultFleetLease
	}
	for _, r := range records {
		if r.State == "active" || r.State == "parked" {
			doc.Fleet.Claims++
		}
	}
	setClaims(doc, func(change string) *Claim {
		r, ok := records[change]
		if !ok {
			return nil
		}
		return &Claim{Machine: r.Machine, State: fleetState(r, lease, t), Epoch: r.Epoch, RenewedAt: timestamp(r.RenewedAt), Run: r.Run}
	})
}

func readTeam(ctx context.Context, doc *Document, root string, cfg *config.Config) {
	read := &ClaimsRead{Source: "forge"}
	doc.ClaimsRead = read
	if TeamClaims == nil {
		read.Stale, read.Error = true, "team claims are not available in this build"
		return
	}
	snap, err := TeamClaims(ctx, root, cfg, now())
	read.At, read.Stale, read.Error = timestamp(snap.FetchedAt), snap.Stale, snap.Err
	if err != nil {
		read.Stale = true
		if read.Error == "" {
			read.Error = err.Error()
		}
	}
	setClaims(doc, func(change string) *Claim {
		c, ok := snap.Claims[change]
		if !ok {
			return nil
		}
		return &Claim{Machine: c.Machine, State: "open", Login: c.Login, Run: c.Run}
	})
}

// setClaims puts each active card's claim on it. A claimed change shows its
// claim whether or not a run of it is on this machine, so a claim held by
// a machine that isn't connected is still seen. Archived cards have none.
func setClaims(doc *Document, of func(change string) *Claim) {
	for i := range doc.Cards {
		c := &doc.Cards[i]
		if c.Archived != "" || c.State == string(runs.Merged) {
			continue
		}
		c.Claim = of(strings.TrimSpace(c.Change))
	}
}
