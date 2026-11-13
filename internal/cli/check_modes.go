package cli

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// FleetRef is the ref on the remote that holds fleet claims.
const FleetRef = "refs/tuckerbox/fleet"

// leftoverClaim is a fleet claim found on the remote.
type leftoverClaim struct{ Change, Machine string }

// fleetClaims lists the claims in the remote's fleet ref, or none when the
// remote has no fleet ref. It only reads: the ref's objects are fetched
// without writing any ref or FETCH_HEAD. Package fleet owns writing them.
var fleetClaims = func(ctx context.Context, root, remote string) ([]leftoverClaim, error) {
	out, err := gitOut(ctx, root, "ls-remote", "--", remote, FleetRef)
	if err != nil {
		return nil, err
	}
	sha, _, _ := strings.Cut(strings.TrimSpace(out), "\t")
	if sha == "" {
		return nil, nil
	}
	if _, err := gitOut(ctx, root, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", remote, FleetRef); err != nil {
		return nil, err
	}
	names, err := gitOut(ctx, root, "ls-tree", "--name-only", sha, "claims/")
	if err != nil {
		return nil, err
	}
	var claims []leftoverClaim
	for _, name := range strings.Fields(names) {
		change, ok := strings.CutSuffix(strings.TrimPrefix(name, "claims/"), ".toml")
		if !ok {
			continue
		}
		body, err := gitOut(ctx, root, "show", sha+":"+name)
		if err != nil {
			return nil, err
		}
		claims = append(claims, leftoverClaim{Change: change, Machine: tomlString(body, "machine")})
	}
	slices.SortFunc(claims, func(a, b leftoverClaim) int { return strings.Compare(a.Change, b.Change) })
	return claims, nil
}

// tomlString reads a top-level `key = "value"` line.
func tomlString(body, key string) string {
	for _, line := range strings.Split(body, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// modeLines prints the repository's mode in check, and, outside fleet
// mode, any fleet claim left on the remote from when it was a fleet
// (operating-modes: Changing mode). Leftover claims stay until their run
// ends or tuckerbox claim release removes them; check still passes.
func (a App) modeLines(root string, cfg *config.Config) {
	fmt.Fprintf(a.Stdout, "mode: %s\n", cfg.Mode)
	if cfg.Mode == config.ModeFleet {
		return
	}
	remote := cfg.Shipping.Remote
	if remote == "" {
		remote = "origin"
	}
	if _, err := gitOut(context.Background(), root, "remote", "get-url", "--", remote); err != nil {
		return // no remote, so no fleet ref
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	claims, err := fleetClaims(ctx, root, remote)
	if err != nil {
		fmt.Fprintf(a.Stdout, "fleet claims: cannot read %s on %s: %v\n", FleetRef, remote, err)
		return
	}
	for _, c := range claims {
		machine := c.Machine
		if machine == "" {
			machine = "unknown machine"
		}
		fmt.Fprintf(a.Stdout, "leftover fleet claim on %s (%s); it stays until its run ends or tuckerbox claim release %s\n", c.Change, machine, c.Change)
	}
}
