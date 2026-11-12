// Package settings reads a change's own step settings from its sidecar,
// openspec/changes/<id>/tuckerbox.toml, and checks them against the
// repository's [overrides] allow-list (see [config.Overrides]).
//
// The sidecar may hold, besides title, depends_on, priority and [approval]:
//
//	machines = ["studio", "mini"]   # machines the change prefers, in order
//
//	[steps.build]
//	agent = "codex"
//	model = "gpt-5.5"
//	effort = "high"
//	permissions = "edit"            # a machine permission profile's name
//
// A step table may hold only agent, model, effort and permissions, and
// must name a step the repository defines in [steps]. A change can never
// carry agent arguments or permission flags: those belong in a permission
// profile in machine config. machines has no effect in solo mode.
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
)

// FileName is the sidecar file in a change folder.
const FileName = deps.FileName

// Keys a change may set in a [steps.<step>] table.
const (
	KeyAgent       = "agent"
	KeyModel       = "model"
	KeyEffort      = "effort"
	KeyPermissions = "permissions"
)

// StepKeys lists the keys a [steps.<step>] table may hold, in display order.
var StepKeys = []string{KeyAgent, KeyModel, KeyEffort, KeyPermissions}

// Override is one step's settings in a change's sidecar. An empty field is
// not overridden.
type Override struct {
	Agent, Model, Effort, Permissions string
}

// Get returns the value of key (one of [StepKeys]).
func (o Override) Get(key string) string {
	switch key {
	case KeyAgent:
		return o.Agent
	case KeyModel:
		return o.Model
	case KeyEffort:
		return o.Effort
	case KeyPermissions:
		return o.Permissions
	}
	return ""
}

// Sidecar is the part of a change's tuckerbox.toml this package reads.
// Other keys (such as priority and [approval]) are left to their packages.
type Sidecar struct {
	Title     string
	DependsOn []string
	// Machines lists the machines the change prefers to run on, in order.
	// It has no effect in solo mode.
	Machines []string
	// Steps holds the change's step overrides, by step id.
	Steps map[string]Override
}

// Problem is one thing wrong with a change's settings.
type Problem struct {
	// File is the sidecar, relative to the repository root with forward
	// slashes.
	File string
	// Change is the change id.
	Change string
	// Key is the dotted key in the sidecar, such as "steps.build.agent".
	Key     string
	Message string
}

func (p Problem) String() string {
	var b strings.Builder
	if p.File != "" {
		b.WriteString(p.File)
		b.WriteString(": ")
	}
	if p.Change != "" {
		fmt.Fprintf(&b, "change %s: ", p.Change)
	}
	if p.Key != "" {
		b.WriteString(p.Key)
		b.WriteString(": ")
	}
	b.WriteString(p.Message)
	return b.String()
}

// rawStep is a [steps.<step>] table. Any other key is undecoded and
// reported.
type rawStep struct {
	Agent       *string `toml:"agent"`
	Model       *string `toml:"model"`
	Effort      *string `toml:"effort"`
	Permissions *string `toml:"permissions"`
}

type rawSidecar struct {
	Title     string             `toml:"title"`
	DependsOn []string           `toml:"depends_on"`
	Machines  []string           `toml:"machines"`
	Steps     map[string]rawStep `toml:"steps"`
}

// Parse reads a sidecar's contents. It returns an error only when data is
// not valid TOML or a known key has the wrong type; problems with the
// settings themselves (unknown or forbidden keys in a step table, empty
// values) are returned as problems, with Key set and File and Change
// empty. Values of refused keys are never put in a problem, so a flag
// someone wrote by mistake is not echoed.
func Parse(data []byte) (Sidecar, []Problem, error) {
	var raw rawSidecar
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return Sidecar{}, nil, err
	}
	if t := md.Type("steps"); t != "" && t != "Hash" {
		// The decoder leaves a non-table into a map alone without error.
		return Sidecar{}, nil, fmt.Errorf("steps: must be tables, such as [steps.build], not a %s", strings.ToLower(t))
	}
	sc := Sidecar{Title: raw.Title, Steps: map[string]Override{}}
	var ps []Problem
	add := func(key, format string, args ...any) {
		ps = append(ps, Problem{Key: key, Message: fmt.Sprintf(format, args...)})
	}
	seen := map[string]bool{}
	for _, d := range raw.DependsOn {
		if d = strings.TrimSpace(d); d != "" && !seen[d] {
			seen[d] = true
			sc.DependsOn = append(sc.DependsOn, d)
		}
	}
	for i, m := range raw.Machines {
		if strings.TrimSpace(m) == "" {
			add("machines", "entry %d is empty; list machine names", i+1)
			continue
		}
		sc.Machines = append(sc.Machines, m)
	}

	// Keys in step tables the struct does not know: refused, by name only.
	// A deeper key (such as args.codex = [...] in [steps.build]) is
	// reported by its key in the step table, once: the decoder lists only
	// the leaf when the table in between is implicit, so a deeper key cannot
	// be assumed to have its parent reported.
	reported := map[string]bool{}
	for _, k := range md.Undecoded() {
		if len(k) < 2 || k[0] != "steps" {
			continue // other top-level keys belong to other packages
		}
		if len(k) == 2 {
			continue // a step table itself; its keys are reported below
		}
		k = k[:3]
		if reported[k.String()] {
			continue
		}
		reported[k.String()] = true
		name := k[2]
		if config.IsArgKey(name) {
			add(k.String(), "agent arguments and permission flags are not allowed in a change; they belong in a permission profile in machine config, which a change may name with permissions = \"<profile>\"")
		} else {
			add(k.String(), "not allowed in a change; a step table may set only %s", strings.Join(StepKeys, ", "))
		}
	}

	for _, id := range slices.Sorted(maps.Keys(raw.Steps)) {
		rs := raw.Steps[id]
		var o Override
		for _, f := range []struct {
			key string
			src *string
			dst *string
		}{
			{KeyAgent, rs.Agent, &o.Agent},
			{KeyModel, rs.Model, &o.Model},
			{KeyEffort, rs.Effort, &o.Effort},
			{KeyPermissions, rs.Permissions, &o.Permissions},
		} {
			if f.src == nil {
				continue
			}
			if strings.TrimSpace(*f.src) == "" {
				add("steps."+id+"."+f.key, "empty value; remove the key to use the repository's value")
				continue
			}
			*f.dst = *f.src
		}
		sc.Steps[id] = o
	}
	return sc, ps, nil
}

// Read reads and parses change id's sidecar in the checkout at root. A
// missing sidecar is an empty one.
func Read(root, id string) (Sidecar, []Problem, error) {
	data, err := os.ReadFile(filepath.Join(deps.ChangesDir(root), id, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return Sidecar{Steps: map[string]Override{}}, nil, nil
	}
	if err != nil {
		return Sidecar{}, nil, err
	}
	return Parse(data)
}

// Check checks a change's step overrides against the repository config
// cfg: each step must be defined in [steps], each value must be in the
// [overrides] allow-list (a model in the list for the step's effective
// agent), and a permission profile must be usable on this machine, as for
// a repository step. Problems have Key set and File and Change empty.
func Check(cfg *config.Config, sc Sidecar) []Problem {
	var ps []Problem
	add := func(key, format string, args ...any) {
		ps = append(ps, Problem{Key: key, Message: fmt.Sprintf(format, args...)})
	}
	for _, id := range slices.Sorted(maps.Keys(sc.Steps)) {
		o := sc.Steps[id]
		key := "steps." + id
		step, ok := cfg.Step(id)
		if !ok {
			add(key, "unknown step %q; the repository config defines no step %q in [steps]", id, id)
			continue
		}
		agent := step.Agent
		if o.Agent != "" {
			agent = o.Agent
		}
		for _, k := range StepKeys {
			v := o.Get(k)
			if v == "" {
				continue
			}
			if msg := allowed(cfg.Overrides, k, v, agent); msg != "" {
				add(key+"."+k, "%s", msg)
				continue
			}
			if k == KeyPermissions {
				if msg := cfg.ProfileProblem(v, agent); msg != "" {
					add(key+"."+k, "%s", msg)
				}
			}
		}
	}
	return ps
}

// allowed returns "" when the allow-list o lets a change set key to v on a
// step whose effective agent is agent, else why not, with the allowed
// values.
func allowed(o *config.Overrides, key, v, agent string) string {
	if o == nil {
		return fmt.Sprintf("%s %q: the repository allows no change overrides; list allowed values under [overrides] in .tuckerbox/config.toml", key, v)
	}
	var list []string
	what := key
	switch key {
	case KeyAgent:
		list, what = o.Agents, "agents"
	case KeyEffort:
		list, what = o.Efforts, "efforts"
	case KeyPermissions:
		list, what = o.Permissions, "permissions"
	case KeyModel:
		list = o.Models[agent]
		if slices.Contains(list, v) {
			return ""
		}
		if len(list) == 0 {
			return fmt.Sprintf("model %q is not an allowed model for %q; [overrides] models allows no models for %q", v, agent, agent)
		}
		return fmt.Sprintf("model %q is not an allowed model for %q; allowed: %s", v, agent, strings.Join(list, ", "))
	}
	if slices.Contains(list, v) {
		return ""
	}
	if len(list) == 0 {
		return fmt.Sprintf("%s %q: the repository allows no change to override %s; add [overrides] %s to .tuckerbox/config.toml", key, v, key, what)
	}
	return fmt.Sprintf("%s %q is not allowed; allowed %s: %s", key, v, what, strings.Join(list, ", "))
}

// CheckAll reads the sidecar of every active change in the checkout at
// root and checks it against cfg. A sidecar that is not valid TOML is
// skipped, because [deps.Check] already reports it; a valid one whose
// settings have the wrong type is reported here. When cfg is nil (the config
// could not be loaded) only the sidecars' own problems are reported.
func CheckAll(root string, cfg *config.Config) ([]Problem, error) {
	active, err := deps.Active(root)
	if err != nil {
		return nil, err
	}
	var out []Problem
	for _, id := range active {
		sc, ps, err := Read(root, id)
		if err != nil {
			if _, derr := deps.Read(root, id); derr != nil {
				continue // reported by deps.Check
			}
			ps = []Problem{{Message: err.Error()}}
		}
		if cfg != nil {
			ps = append(ps, Check(cfg, sc)...)
		}
		for _, p := range ps {
			p.File = "openspec/changes/" + id + "/" + FileName
			p.Change = id
			out = append(out, p)
		}
	}
	return out, nil
}
