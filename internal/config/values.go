package config

import (
	"maps"
	"slices"
	"strings"
)

// FileInfo describes one config file as it was read.
type FileInfo struct {
	// Path is the file's path; "" when there is no such file to read
	// (no machine config folder).
	Path string
	// Exists is false when the file is absent.
	Exists bool
	// Revision is the SHA-256 of the file's bytes ([Revision]), or ""
	// when it is absent.
	Revision string
	// Error is the problem that stopped the file being decoded, or nil.
	Error *Problem
}

// Files are the files a [Config] was read from.
type Files struct {
	Repo, Machine, Credentials FileInfo
}

// Value is one key's effective value and where it came from.
type Value struct {
	// Key is the concrete dotted key, such as "steps.review.effort".
	Key string
	// Def is the key's row in [Keys].
	Def Key
	// Value is the effective value: string, int64, bool or []string, or
	// the built-in default, or nil when the key is unset and has none.
	// For a secret it is only "set" or "not set".
	Value any
	// Set is true when a file sets the key (or, for a secret, when a
	// token is stored).
	Set bool
	// Source is SourceRepo, SourceMachine or SourceDefault.
	Source Source
	// File is the path of the file the value came from; "" for a default.
	File string
	// Overridden is the machine value a repository value hides, when
	// HasOverridden is true.
	Overridden    any
	HasOverridden bool
	// TokenSource, for a secret, is where the token in use comes from
	// ([forge.StoredSource], a variable's name, gh or glab), or "" when
	// none is known. Config fills in only the credentials file; callers
	// that look up tokens fill in the rest.
	TokenSource string
}

// leaf is one key set in one file.
type leaf struct {
	segs []string
	def  Key
	val  any
}

// flatten lists the keys of the key table a decoded file sets, by
// concrete key. Secret rows are skipped: a token in config.toml is refused
// and must never become a value.
func flatten(tree map[string]any) map[string]leaf {
	out := map[string]leaf{}
	var walk func(segs []string, v any)
	walk = func(segs []string, v any) {
		if k, ok := lookupSegs(segs); ok {
			if !k.Secret {
				out[JoinKey(segs)] = leaf{segs: slices.Clone(segs), def: k, val: native(v)}
			}
			return
		}
		if m, ok := v.(map[string]any); ok {
			for _, name := range slices.Sorted(maps.Keys(m)) {
				walk(append(segs, name), m[name])
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(tree)) {
		walk([]string{name}, tree[name])
	}
	return out
}

// native turns a decoded TOML value into string, int64, bool or []string
// where it can; other shapes are kept as decoded.
func native(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]string, 0, len(list))
	for _, e := range list {
		s, ok := e.(string)
		if !ok {
			return v
		}
		out = append(out, s)
	}
	return out
}

// instance is the part of a concrete key up to and including its last
// placeholder segment, such as "steps.review"; "" for an unpatterned key.
func instance(def Key, segs []string) (prefix []string, rest []string) {
	last := -1
	for i, s := range def.segs {
		if isPlaceholder(s) {
			last = i
		}
	}
	if last < 0 {
		return nil, def.segs
	}
	return segs[:last+1], def.segs[last+1:]
}

// values works out every key's effective value and source from the two
// parsed files (either may be nil) and the credentials file.
func values(m, r *file, creds *credentials, workflow []string) []Value {
	var mLeaves, rLeaves map[string]leaf
	if m != nil {
		mLeaves = flatten(m.tree)
	}
	if r != nil {
		rLeaves = flatten(r.tree)
	}
	// A repository agent replaces a machine agent of the same name as a
	// whole (see resolve), so none of the machine's keys for it apply.
	for k, l := range mLeaves {
		if l.segs[0] != "agents" {
			continue
		}
		for _, rl := range rLeaves {
			if rl.segs[0] == "agents" && rl.segs[1] == l.segs[1] {
				delete(mLeaves, k)
				break
			}
		}
	}

	// Every concrete key to report: each unpatterned row, every key either
	// file sets, and every row of each table instance either file names
	// (so steps.review lists all its keys, set or not).
	keys := map[string]Key{}
	addInstance := func(def Key, segs []string) {
		prefix, _ := instance(def, segs)
		for _, k := range Keys {
			if len(k.segs) <= len(prefix) || !k.prefixOf(prefix) {
				continue
			}
			rest := k.segs[len(prefix):]
			if slices.ContainsFunc(rest, isPlaceholder) {
				continue
			}
			if p, _ := instance(k, k.segs); len(p) != len(prefix) {
				continue
			}
			full := append(slices.Clone(prefix), rest...)
			keys[JoinKey(full)] = k
		}
	}
	for _, k := range Keys {
		if !k.Patterned() {
			keys[k.Path] = k
		}
	}
	for _, ls := range []map[string]leaf{mLeaves, rLeaves} {
		for key, l := range ls {
			keys[key] = l.def
			addInstance(l.def, l.segs)
		}
	}
	stepRow, _ := lookupSegs([]string{"steps", "_", "agent"})
	for _, id := range workflow {
		addInstance(stepRow, []string{"steps", id, "agent"})
	}
	tokenRow, _ := lookupSegs([]string{"forges", "_", "token"})
	if creds != nil {
		for host := range creds.tokens {
			addInstance(tokenRow, []string{"forges", host, "token"})
		}
	}

	path := func(f *file) string {
		if f == nil {
			return ""
		}
		return f.path
	}
	out := make([]Value, 0, len(keys))
	for key, def := range keys {
		v := Value{Key: key, Def: def, Value: def.Default, Source: SourceDefault}
		if def.Secret {
			v.Value = "not set"
			segs, _ := SplitKey(key)
			if creds != nil {
				if _, ok := creds.tokens[segs[1]]; ok {
					v = Value{Key: key, Def: def, Value: "set", Set: true, Source: SourceMachine, File: creds.info.Path, TokenSource: "credentials file"}
				}
			}
			out = append(out, v)
			continue
		}
		rl, inRepo := rLeaves[key]
		ml, inMachine := mLeaves[key]
		switch {
		case inRepo:
			v.Value, v.Set, v.Source, v.File = rl.val, true, SourceRepo, path(r)
			if inMachine {
				v.Overridden, v.HasOverridden = ml.val, true
			}
		case inMachine:
			v.Value, v.Set, v.Source, v.File = ml.val, true, SourceMachine, path(m)
		}
		out = append(out, v)
	}
	order := map[string]int{}
	for i, k := range Keys {
		order[k.Path] = i
	}
	slices.SortFunc(out, func(a, b Value) int {
		ap, _ := instance(a.Def, splitOrNil(a.Key))
		bp, _ := instance(b.Def, splitOrNil(b.Key))
		// Group by table instance in key-table order, then by row.
		if c := order[firstRowOf(a.Def)] - order[firstRowOf(b.Def)]; c != 0 {
			return c
		}
		if c := strings.Compare(JoinKey(ap), JoinKey(bp)); c != 0 {
			return c
		}
		return order[a.Def.Path] - order[b.Def.Path]
	})
	return out
}

func splitOrNil(key string) []string {
	segs, _ := SplitKey(key)
	return segs
}

// firstRowOf is the first row of the key table that shares def's
// pattern prefix, so all rows of one table sort together.
func firstRowOf(def Key) string {
	prefix, _ := instance(def, def.segs)
	if prefix == nil {
		return def.Path
	}
	for _, k := range Keys {
		if p, _ := instance(k, k.segs); slices.Equal(p, prefix) {
			return k.Path
		}
	}
	return def.Path
}

// Value returns the effective value of a concrete key.
func (c *Config) Value(key string) (Value, bool) {
	segs, err := SplitKey(key)
	if err != nil {
		return Value{}, false
	}
	key = JoinKey(segs)
	for _, v := range c.Values {
		if v.Key == key {
			return v, true
		}
	}
	return Value{}, false
}
