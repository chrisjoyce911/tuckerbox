package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/edits"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
)

const setUsage = "usage: tuckerbox set <change> <key>=<value> | tuckerbox set <change> --unset <key> [--json]\n" +
	"keys: step.<step>.agent, step.<step>.model, step.<step>.effort, step.<step>.permissions, machines (comma-separated)"

// SetVersion is the version of tuckerbox set --json.
const SetVersion = 1

// setResult is tuckerbox set --json.
type setResult struct {
	Version int    `json:"version"`
	Change  string `json:"change"`
	Key     string `json:"key"`
	// Value is the value set; "" for --unset.
	Value string `json:"value,omitempty"`
	Unset bool   `json:"unset"`
	// Changed is false when the sidecar already said so.
	Changed bool `json:"changed"`
	// Dir is where the sidecar was written: the change's worktree or the
	// repository checkout.
	Dir      string `json:"dir"`
	Worktree bool   `json:"worktree"`
	// File is the sidecar, relative to Dir.
	File      string   `json:"file"`
	Committed bool     `json:"committed"`
	Warnings  []string `json:"warnings,omitempty"`
	Note      string   `json:"note,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// set: tuckerbox set <change> <key>=<value> | --unset <key> [--json]
func (a App) set(args []string) int {
	var asJSON bool
	var unset string
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, map[string]*string{"unset": &unset})
	if err != nil {
		return a.usageErr("set", err.Error()+"\n"+setUsage)
	}
	var keyArg, value string
	switch {
	case unset != "" && len(pos) == 1:
		keyArg = unset
	case unset == "" && len(pos) == 2:
		var ok bool
		keyArg, value, ok = strings.Cut(pos[1], "=")
		if !ok {
			return a.usageErr("set", fmt.Sprintf("%q is not <key>=<value>\n%s", pos[1], setUsage))
		}
	default:
		return a.usageErr("set", setUsage)
	}
	change := pos[0]
	res := setResult{Version: SetVersion, Change: change, Key: keyArg, Value: value, Unset: unset != ""}
	fail := func(format string, args ...any) int {
		msg := fmt.Sprintf(format, args...)
		if asJSON {
			res.Error, res.Changed, res.Warnings = msg, false, nil
			a.writeJSON(res)
		}
		return a.errf("set", "%s", msg)
	}
	k, err := settings.ParseKey(keyArg)
	if err != nil {
		return fail("%v; nothing changed", err)
	}
	res.Key = k.String()

	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return fail("%v", err)
	}
	ctx := context.Background()
	p, err := edits.Locate(ctx, wd, change)
	if err != nil {
		return fail("%v", err)
	}
	res.Dir, res.Worktree = p.Dir, p.Worktree
	res.File = filepath.ToSlash(filepath.Join("openspec", "changes", change, settings.FileName))
	path := filepath.Join(p.ChangeDir(), settings.FileName)

	var refused []settings.Problem
	err = edits.WithChangeLock(ctx, p, "tuckerbox set", func() error {
		return withSidecar(ctx, p.Dir, func() error {
			old, err := os.ReadFile(path)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			var out []byte
			if res.Unset {
				out, res.Changed, err = settings.UnsetValue(old, k)
			} else {
				out, res.Changed, err = settings.SetValue(old, k, value)
			}
			if err != nil || !res.Changed {
				return err
			}
			if !res.Unset && k.Step != "" {
				// Judged against the repository config where the edit is
				// made, which a run there judges it against too.
				cfg, err := a.placeConfig(p.Dir)
				if err != nil {
					return err
				}
				before, _, err := settings.Parse(old)
				if err != nil {
					return err
				}
				after, _, err := settings.Parse(out)
				if err != nil {
					return err
				}
				if refused = settings.CheckSet(cfg, before, after, k); len(refused) > 0 {
					return nil
				}
				res.Warnings = settings.ProfileWarnings(cfg, after, k)
			}
			if len(bytes.TrimSpace(out)) == 0 {
				return os.Remove(path)
			}
			return approval.WriteSidecar(path, out)
		})
	})
	if err != nil {
		return fail("%v; nothing changed", err)
	}
	if len(refused) > 0 {
		msgs := make([]string, len(refused))
		for i, pr := range refused {
			pr.Change = change
			msgs[i] = pr.String()
		}
		return fail("%s; nothing changed", strings.Join(msgs, "; "))
	}

	if res.Changed {
		res.Note = fmt.Sprintf("not committed: it counts on other machines, and for runs that start without a worktree, only once %s is committed and pushed", res.File)
	}
	if asJSON {
		return a.writeJSON(res)
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(a.Stderr, "tuckerbox set: warning: %s on this machine\n", w)
	}
	switch {
	case !res.Changed && res.Unset:
		fmt.Fprintf(a.Stdout, "%s is not set for %s; nothing changed in %s\n", k, change, p.Describe())
		return 0
	case !res.Changed:
		fmt.Fprintf(a.Stdout, "%s of %s is already %s; nothing changed in %s\n", k, change, value, p.Describe())
		return 0
	case res.Unset && k.Step != "":
		fmt.Fprintf(a.Stdout, "removed %s from %s in %s; step %s uses the repository's %s again\n", k, change, p.Describe(), k.Step, k.Field)
	case res.Unset:
		fmt.Fprintf(a.Stdout, "removed %s from %s in %s\n", k, change, p.Describe())
	default:
		fmt.Fprintf(a.Stdout, "set %s=%s for %s in %s\n", k, value, change, p.Describe())
	}
	fmt.Fprintln(a.Stdout, res.Note)
	return 0
}

// placeConfig loads the repository config, with machine config, of the
// checkout or worktree at dir.
func (a App) placeConfig(dir string) (*config.Config, error) {
	env, _ := a.configEnv()
	opts, err := config.Discover(env, dir)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(opts)
	var problems config.Problems
	if errors.As(err, &problems) {
		return nil, fmt.Errorf("the config has problems; fix them first (tuckerbox check)")
	}
	return cfg, err
}
