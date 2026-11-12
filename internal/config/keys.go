package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// Scope says which config file may set a key.
type Scope string

// Key scopes.
//
//   - ScopeRepo: repository config only, so every machine applies the same
//     rule.
//   - ScopeMachine: machine config (or the credentials file) only: a fact
//     about this machine, or something a repository must not grant itself.
//   - ScopeEither: a machine default that repository config overrides.
const (
	ScopeRepo    Scope = "repo"
	ScopeMachine Scope = "machine"
	ScopeEither  Scope = "either"
)

// KeyType is the type of a key's value.
type KeyType string

// Key types. A list is written comma-separated on the command line. An enum
// is a string with a fixed set of values ([Key.Allowed]); a duration is a
// string such as "60s".
const (
	TypeString   KeyType = "string"
	TypeInt      KeyType = "int"
	TypeBool     KeyType = "bool"
	TypeList     KeyType = "list"
	TypeEnum     KeyType = "enum"
	TypeDuration KeyType = "duration"
)

// Key is one row of the key table: one setting either config file may hold.
type Key struct {
	// Path is the dotted key. A segment written <name> is a placeholder
	// that matches any name: "steps.<id>.effort", "forges.<host>.token".
	Path string
	Type KeyType
	// Allowed lists the values a TypeEnum key may take; nil otherwise.
	Allowed []string
	Scope   Scope
	// Default is the built-in value used when neither file sets the key
	// (string, int64, bool or []string), or nil when there is none.
	Default any
	// Secret keys (forge tokens) are never shown, only whether they are
	// set. They are stored in the credentials file, not config.toml.
	Secret bool

	segs []string
}

var (
	agentBackends = []string{BackendAuto, BackendTerminal, BackendHeadless}
	shipModes     = []string{string(ShipLocal), string(ShipMR), string(ShipMerge)}
	forgeKinds    = []string{"github", "gitlab"}
)

// Keys is the key table: every key `tuckerbox check` knows, with its scope.
// check, `tuckerbox config` and the Settings view all read it, so they
// never disagree about where a key belongs. A later change that adds a key
// adds a row here; a test fails when the loader accepts a key this table
// lacks.
var Keys = []Key{
	{Path: "machine", Type: TypeString, Scope: ScopeMachine},
	{Path: "editor", Type: TypeString, Scope: ScopeMachine},
	{Path: "agent_backend", Type: TypeEnum, Allowed: agentBackends, Scope: ScopeMachine, Default: BackendAuto},
	{Path: "max_parallel", Type: TypeInt, Scope: ScopeEither, Default: int64(DefaultMaxParallel)},
	{Path: "ship", Type: TypeEnum, Allowed: shipModes, Scope: ScopeEither, Default: string(ShipLocal)},
	{Path: "require_approval", Type: TypeBool, Scope: ScopeRepo, Default: true},

	{Path: "workflow.steps", Type: TypeList, Scope: ScopeEither},
	{Path: "steps.<id>.agent", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.role", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.permissions", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.model", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.effort", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.on_reject", Type: TypeString, Scope: ScopeEither},
	{Path: "steps.<id>.max_rounds", Type: TypeInt, Scope: ScopeEither, Default: int64(DefaultMaxRounds)},

	{Path: "agents.<name>.command", Type: TypeList, Scope: ScopeEither},
	{Path: "agents.<name>.interactive", Type: TypeList, Scope: ScopeEither},
	{Path: "agents.<name>.model", Type: TypeString, Scope: ScopeEither},
	{Path: "agents.<name>.effort", Type: TypeString, Scope: ScopeEither},

	{Path: "permissions.<name>.args.<agent>", Type: TypeList, Scope: ScopeMachine},
	{Path: "permissions.<name>.allowed_remotes", Type: TypeList, Scope: ScopeMachine},

	{Path: "board.columns", Type: TypeEnum, Allowed: []string{string(BoardCompact), string(BoardSteps)}, Scope: ScopeEither, Default: string(BoardCompact)},

	{Path: "worktree.base", Type: TypeString, Scope: ScopeEither},
	{Path: "worktree.path", Type: TypeString, Scope: ScopeEither, Default: DefaultWorktreePath},
	{Path: "worktree.branch", Type: TypeString, Scope: ScopeEither, Default: DefaultBranch},

	{Path: "autopilot.scope", Type: TypeEnum, Allowed: []string{AutopilotGroup, AutopilotChange}, Scope: ScopeEither, Default: AutopilotGroup},
	{Path: "autopilot.interval", Type: TypeDuration, Scope: ScopeEither, Default: DefaultAutopilotInterval.String()},

	{Path: "overrides.agents", Type: TypeList, Scope: ScopeRepo},
	{Path: "overrides.models.<agent>", Type: TypeList, Scope: ScopeRepo},
	{Path: "overrides.efforts", Type: TypeList, Scope: ScopeRepo},
	{Path: "overrides.permissions", Type: TypeList, Scope: ScopeRepo},

	{Path: "shipping.remote", Type: TypeString, Scope: ScopeRepo, Default: "origin"},
	{Path: "shipping.fix_ci", Type: TypeString, Scope: ScopeRepo},
	{Path: "shipping.fix_review", Type: TypeString, Scope: ScopeRepo},
	{Path: "shipping.max_rounds", Type: TypeInt, Scope: ScopeRepo, Default: int64(3)},
	{Path: "shipping.merge_method", Type: TypeEnum, Allowed: []string{"merge", "squash", "rebase"}, Scope: ScopeRepo, Default: "merge"},
	{Path: "shipping.archive", Type: TypeBool, Scope: ScopeRepo, Default: true},
	{Path: "shipping.labels", Type: TypeList, Scope: ScopeRepo},
	{Path: "shipping.review_from", Type: TypeList, Scope: ScopeRepo},
	{Path: "shipping.wait_timeout", Type: TypeDuration, Scope: ScopeRepo, Default: "2h0m0s"},

	{Path: "forge.kind", Type: TypeEnum, Allowed: forgeKinds, Scope: ScopeRepo},
	{Path: "forge.api_url", Type: TypeString, Scope: ScopeRepo},

	{Path: "forges.<host>.kind", Type: TypeEnum, Allowed: forgeKinds, Scope: ScopeMachine},
	{Path: "forges.<host>.token_env", Type: TypeString, Scope: ScopeMachine},
	{Path: "forges.<host>.borrow_cli_token", Type: TypeBool, Scope: ScopeMachine, Default: false},
	{Path: "forges.<host>.token", Type: TypeString, Scope: ScopeMachine, Secret: true},
}

func init() {
	for i := range Keys {
		Keys[i].segs = strings.Split(Keys[i].Path, ".")
	}
}

// Patterned reports whether the key's path holds a placeholder.
func (k Key) Patterned() bool { return strings.Contains(k.Path, "<") }

// Top is the key's first segment, the top-level key or table it lives in.
func (k Key) Top() string { return k.segs[0] }

// match reports whether the concrete key segments segs name this row.
func (k Key) match(segs []string) bool {
	if len(segs) != len(k.segs) {
		return false
	}
	for i, s := range k.segs {
		if !isPlaceholder(s) && s != segs[i] {
			return false
		}
	}
	return true
}

// prefixOf reports whether segs is a strict prefix (a table) of this row.
func (k Key) prefixOf(segs []string) bool {
	if len(segs) >= len(k.segs) {
		return false
	}
	for i, s := range segs {
		if !isPlaceholder(k.segs[i]) && k.segs[i] != s {
			return false
		}
	}
	return true
}

func isPlaceholder(seg string) bool {
	return strings.HasPrefix(seg, "<") && strings.HasSuffix(seg, ">")
}

// LookupKey returns the key table row for a concrete dotted key, such as
// "steps.review.effort" or `forges."git.example.org".token`.
func LookupKey(key string) (Key, bool) {
	segs, err := SplitKey(key)
	if err != nil {
		return Key{}, false
	}
	return lookupSegs(segs)
}

func lookupSegs(segs []string) (Key, bool) {
	for _, k := range Keys {
		if k.match(segs) {
			return k, true
		}
	}
	return Key{}, false
}

// IsTable reports whether key names a table that holds keys of the key
// table, such as "steps.review" or "shipping".
func IsTable(key string) bool {
	segs, err := SplitKey(key)
	if err != nil {
		return false
	}
	for _, k := range Keys {
		if k.prefixOf(segs) {
			return true
		}
	}
	return false
}

// SplitKey splits a dotted key into its segments. A segment in double
// quotes keeps its dots: `forges."git.example.org".token`.
func SplitKey(key string) ([]string, error) {
	var segs []string
	rest := key
	for {
		var seg string
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				return nil, fmt.Errorf("key %q: unterminated quote", key)
			}
			seg, rest = rest[1:end+1], rest[end+2:]
		} else {
			i := strings.IndexByte(rest, '.')
			if i < 0 {
				seg, rest = rest, ""
			} else {
				seg, rest = rest[:i], rest[i:]
			}
			if strings.ContainsAny(seg, `" =`) {
				return nil, fmt.Errorf("key %q: invalid segment %q", key, seg)
			}
		}
		if seg == "" {
			return nil, fmt.Errorf("key %q: empty segment", key)
		}
		segs = append(segs, seg)
		if rest == "" {
			return segs, nil
		}
		if !strings.HasPrefix(rest, ".") {
			return nil, fmt.Errorf("key %q: expected . after a quoted segment", key)
		}
		rest = rest[1:]
		if rest == "" {
			return nil, fmt.Errorf("key %q: ends with a dot", key)
		}
	}
}

// JoinKey joins key segments into a dotted key, quoting a segment that is
// not a bare TOML key (such as a host name with dots).
func JoinKey(segs []string) string { return toml.Key(segs).String() }

// Revision is a config file's revision: the SHA-256 of its bytes, in hex.
// An absent file has revision "".
func Revision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// scopeTops maps each top-level key whose rows are all repo or all machine
// scope to that scope. Every row under one top-level key has the same
// scope (a test checks it), so check can refuse a whole table in the
// wrong file.
func scopeTops() map[string]Scope {
	out := map[string]Scope{}
	for _, k := range Keys {
		out[k.Top()] = k.Scope
	}
	return out
}

// wrongFileMessages are the messages check gives for a top-level key set
// in the wrong file, where a generic one would say less. Keys not listed
// get "belongs in machine config" or "belongs in repository config".
var wrongFileMessages = map[string]string{
	"permissions":      "permission profiles are machine-only; define them in machine config and name them from a step with permissions = \"<profile>\"",
	"editor":           "editor is machine-only; set it in machine config",
	"agent_backend":    "agent_backend belongs in machine config: whether herdr-core is installed and running is a fact about the machine",
	"require_approval": "require_approval belongs in repository config (.tuckerbox/config.toml), so every machine applies the same gate",
	"overrides":        "[overrides] belongs in repository config (.tuckerbox/config.toml), so every machine allows the same change overrides",
}

// WrongFileMessage says where a key of the given scope belongs, for a key
// set in the other file. top is its top-level key.
func WrongFileMessage(top string, scope Scope) string {
	if msg, ok := wrongFileMessages[top]; ok {
		return msg
	}
	if scope == ScopeMachine {
		return "belongs in machine config"
	}
	return "belongs in repository config (.tuckerbox/config.toml)"
}
