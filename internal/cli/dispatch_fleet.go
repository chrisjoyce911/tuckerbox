package cli

import (
	"context"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/dispatch"
)

// fleetRef is the ref holding fleet claims on the remote.
const fleetRef = "refs/tuckerbox/fleet"

// fleetSkew is how far clocks may disagree before an active claim reads as
// expired.
const fleetSkew = time.Minute

// fleetClaims fetches the fleet ref from remote and reads its claims, by
// change, each with the state a reader works out (expired: active and not
// renewed within lease, allowing for clock skew). A remote without the
// ref has no claims.
//
// It is a var so tests may replace it; it moves to the fleet package's
// store once that lands.
var fleetClaims = func(ctx context.Context, root, remote string, lease time.Duration, now time.Time) (map[string]dispatch.Claim, error) {
	out := map[string]dispatch.Claim{}
	if _, err := gitOut(ctx, root, "fetch", "--quiet", remote, "+"+fleetRef+":"+fleetRef); err != nil {
		if _, lerr := gitOut(ctx, root, "ls-remote", "--exit-code", remote, fleetRef); lerr != nil {
			return out, nil // no claims yet
		}
		return nil, err
	}
	names, err := gitOut(ctx, root, "ls-tree", "--name-only", fleetRef+":claims")
	if err != nil {
		return out, nil
	}
	for _, name := range strings.Fields(names) {
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		data, err := gitOut(ctx, root, "show", fleetRef+":claims/"+name)
		if err != nil {
			return nil, err
		}
		var c struct {
			Change    string    `toml:"change"`
			Machine   string    `toml:"machine"`
			State     string    `toml:"state"`
			RenewedAt time.Time `toml:"renewed_at"`
		}
		if _, err := toml.Decode(data, &c); err != nil {
			continue
		}
		id := c.Change
		if id == "" {
			id = strings.TrimSuffix(path.Base(name), ".toml")
		}
		state := c.State
		if state == dispatch.ClaimActive && lease > 0 && now.Sub(c.RenewedAt) > lease+fleetSkew {
			state = dispatch.ClaimExpired
		}
		out[id] = dispatch.Claim{Machine: c.Machine, State: state}
	}
	return out, nil
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	return string(b), err
}
