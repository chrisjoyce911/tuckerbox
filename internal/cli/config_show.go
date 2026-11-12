package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

const configUsage = `usage:
  tuckerbox config show [--json]
  tuckerbox config get <key> [--json]
`

// configCmd runs tuckerbox config <subcommand>.
func (a App) configCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stderr, configUsage)
		return 2
	}
	switch args[0] {
	case "show":
		return a.configShow(args[1:])
	case "get":
		return a.configGet(args[1:])
	default:
		fmt.Fprintf(a.Stderr, "tuckerbox config: unknown subcommand %q\n%s", args[0], configUsage)
		return 2
	}
}

// configView is what show and get read: the loaded config with every
// secret's token source looked up, and this machine's name.
type configView struct {
	cfg      *config.Config
	root     string
	machine  string
	problems config.Problems
}

func (a App) loadConfigView(cmd string) (*configView, int) {
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		fmt.Fprintf(a.Stderr, "tuckerbox config %s: %v\n", cmd, err)
		return nil, 1
	}
	opts, err := config.Discover(env, wd)
	if err != nil {
		fmt.Fprintf(a.Stderr, "tuckerbox config %s: %v\n", cmd, err)
		return nil, 1
	}
	cfg, err := config.Load(opts)
	var problems config.Problems
	if err != nil && !errors.As(err, &problems) {
		fmt.Fprintf(a.Stderr, "tuckerbox config %s: %v\n", cmd, err)
		return nil, 1
	}
	v := &configView{cfg: cfg, root: opts.RepoRoot, machine: cfg.Machine, problems: problems}
	if v.machine == "" {
		v.machine, _ = os.Hostname()
	}
	a.lookupTokenSources(cfg, env.Getenv)
	return v, 0
}

// lookupTokenSources fills in, for each forge token key, where the token
// in use comes from: the credentials file, a variable or gh/glab. It also
// lists the repository's own forge host. Token values are never kept.
func (a App) lookupTokenSources(cfg *config.Config, getenv func(string) string) {
	hasHost := func(host string) bool {
		return slices.ContainsFunc(cfg.Values, func(v config.Value) bool {
			return v.Def.Secret && v.Key == config.JoinKey([]string{"forges", host, "token"})
		})
	}
	if host := cfg.ForgeTarget.Host; host != "" && !hasHost(host) {
		def, _ := config.LookupKey(config.JoinKey([]string{"forges", host, "token"}))
		cfg.Values = append(cfg.Values, config.Value{Key: config.JoinKey([]string{"forges", host, "token"}), Def: def, Value: "not set", Source: config.SourceDefault})
	}
	borrow := a.BorrowToken
	if borrow == nil {
		borrow = forge.BorrowCLIToken
	}
	for i, v := range cfg.Values {
		if !v.Def.Secret {
			continue
		}
		segs, _ := config.SplitKey(v.Key)
		host := segs[1]
		target := forge.Target{Host: host, Kind: cfg.Forges[host].Kind}
		if target.Kind == "" && host == cfg.ForgeTarget.Host {
			target.Kind = cfg.ForgeTarget.Kind
		}
		if target.Kind == "" {
			switch host {
			case "github.com":
				target.Kind = forge.GitHub
			case "gitlab.com":
				target.Kind = forge.GitLab
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tok, err := forge.LookupToken(ctx, target, cfg.Forges[host].Credentials(), getenv, borrow)
		cancel()
		if err != nil || !tok.Available() {
			continue
		}
		cfg.Values[i].TokenSource = tok.Source
		if !v.Set {
			cfg.Values[i].Value = "set"
		}
	}
}

func (a App) configShow(args []string) int {
	asJSON := false
	for _, arg := range args {
		if arg != "--json" {
			fmt.Fprintf(a.Stderr, "tuckerbox config show: unknown argument %q\n%s", arg, configUsage)
			return 2
		}
		asJSON = true
	}
	v, code := a.loadConfigView("show")
	if v == nil {
		return code
	}
	if asJSON {
		return a.writeConfigJSON(v, v.cfg.Values)
	}
	a.writeConfigText(v, v.cfg.Values, true)
	return 0
}

func (a App) configGet(args []string) int {
	asJSON := false
	var key string
	for _, arg := range args {
		switch {
		case arg == "--json":
			asJSON = true
		case key == "" && !strings.HasPrefix(arg, "-"):
			key = arg
		default:
			fmt.Fprintf(a.Stderr, "tuckerbox config get: unexpected argument %q\n%s", arg, configUsage)
			return 2
		}
	}
	if key == "" {
		fmt.Fprint(a.Stderr, "tuckerbox config get: name a key\n", configUsage)
		return 2
	}
	v, code := a.loadConfigView("get")
	if v == nil {
		return code
	}
	segs, err := config.SplitKey(key)
	var picked []config.Value
	if err == nil {
		key = config.JoinKey(segs)
		for _, val := range v.cfg.Values {
			if val.Key == key || strings.HasPrefix(val.Key, key+".") {
				picked = append(picked, val)
			}
		}
	}
	if len(picked) == 0 {
		msg := fmt.Sprintf("tuckerbox config get: unknown key %q", key)
		if near := nearestKey(key, v.cfg.Values); near != "" {
			msg += fmt.Sprintf("; did you mean %s?", near)
		}
		fmt.Fprintln(a.Stderr, msg)
		return 1
	}
	if asJSON {
		return a.writeConfigJSON(v, picked)
	}
	if len(picked) == 1 && picked[0].Key == key {
		fmt.Fprintf(a.Stdout, "%s (%s)\n", formatValue(picked[0]), sourceText(picked[0]))
		return 0
	}
	a.writeConfigText(v, picked, false)
	return 0
}

// formatValue gives a value as it is written on the command line: lists
// comma-separated, an unset key as "(not set)".
func formatValue(v config.Value) string {
	switch x := v.Value.(type) {
	case nil:
		return "(not set)"
	case []string:
		return strings.Join(x, ",")
	default:
		return fmt.Sprint(x)
	}
}

func sourceText(v config.Value) string {
	if v.Def.Secret {
		if v.TokenSource == "" {
			return "no token"
		}
		return v.TokenSource
	}
	s := string(v.Source)
	if v.HasOverridden {
		s += "; machine sets " + formatValue(config.Value{Value: v.Overridden})
	}
	return s
}

func (a App) writeConfigText(v *configView, vals []config.Value, header bool) {
	if header {
		for _, f := range []struct {
			name string
			info config.FileInfo
		}{{"repository", v.cfg.Files.Repo}, {"machine", v.cfg.Files.Machine}, {"credentials", v.cfg.Files.Credentials}} {
			if f.info.Path == "" {
				continue
			}
			state := "absent"
			switch {
			case f.info.Error != nil:
				state = "does not parse: " + f.info.Error.String()
			case f.info.Revision != "":
				state = "revision " + f.info.Revision[:12]
			case f.info.Exists:
				state = "present"
			}
			fmt.Fprintf(a.Stdout, "%s: %s (%s)\n", f.name, displayPath(v.root, f.info.Path), state)
		}
		fmt.Fprintln(a.Stdout)
	}
	var repo, machine []config.Value
	for _, val := range vals {
		if val.Def.Scope == config.ScopeMachine {
			machine = append(machine, val)
		} else {
			repo = append(repo, val)
		}
	}
	line := func(val config.Value) {
		scope := ""
		if val.Def.Scope != config.ScopeMachine {
			scope = " [" + string(val.Def.Scope) + "]"
		}
		fmt.Fprintf(a.Stdout, "  %s = %s (%s)%s\n", val.Key, formatValue(val), sourceText(val), scope)
	}
	if len(repo) > 0 {
		fmt.Fprintln(a.Stdout, "Repository (machine defaults where the repository sets nothing)")
		for _, val := range repo {
			line(val)
		}
	}
	if len(machine) > 0 {
		if len(repo) > 0 {
			fmt.Fprintln(a.Stdout)
		}
		name := v.machine
		if name == "" {
			name = "this machine"
		}
		fmt.Fprintf(a.Stdout, "%s: this machine only\n", name)
		for _, val := range machine {
			line(val)
		}
	}
	if header && len(v.problems) > 0 {
		fmt.Fprintf(a.Stdout, "\n%d problem(s); run tuckerbox check\n", len(v.problems))
		for _, p := range v.problems {
			p.File = displayPath(v.root, p.File)
			fmt.Fprintln(a.Stdout, "  "+p.String())
		}
	}
}

// JSON shapes of config show --json and config get --json.
type jsonConfigFile struct {
	Path     string         `json:"path"`
	Revision *string        `json:"revision"`
	Error    *jsonFileError `json:"error"`
}

type jsonFileError struct {
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

type jsonOverridden struct {
	Source string `json:"source"`
	Value  any    `json:"value"`
}

type jsonConfigKey struct {
	Key        string          `json:"key"`
	Value      any             `json:"value"`
	Set        bool            `json:"set"`
	Source     string          `json:"source"`
	Scope      string          `json:"scope"`
	File       string          `json:"file"`
	Type       string          `json:"type"`
	Allowed    []string        `json:"allowed"`
	Default    any             `json:"default"`
	Secret     bool            `json:"secret"`
	Overridden *jsonOverridden `json:"overridden,omitempty"`
}

type jsonProblem struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

type jsonConfig struct {
	Machine  string                    `json:"machine"`
	Files    map[string]jsonConfigFile `json:"files"`
	Keys     []jsonConfigKey           `json:"keys"`
	Problems []jsonProblem             `json:"problems"`
}

func jsonFile(info config.FileInfo) jsonConfigFile {
	f := jsonConfigFile{Path: info.Path}
	if info.Revision != "" {
		rev := info.Revision
		f.Revision = &rev
	}
	if info.Error != nil {
		f.Error = &jsonFileError{Message: info.Error.Message, Line: info.Error.Line}
	}
	return f
}

// fileName names the file a value came from as the JSON does: "repo",
// "machine", "credentials" or "" for a default.
func fileName(cfg *config.Config, path string) string {
	switch {
	case path == "":
		return ""
	case path == cfg.Files.Repo.Path:
		return "repo"
	case path == cfg.Files.Machine.Path:
		return "machine"
	case path == cfg.Files.Credentials.Path:
		return "credentials"
	}
	return path
}

func jsonKey(cfg *config.Config, v config.Value) jsonConfigKey {
	typ := string(v.Def.Type)
	if v.Def.Type == config.TypeEnum || v.Def.Type == config.TypeDuration {
		typ = string(config.TypeString)
	}
	k := jsonConfigKey{
		Key: v.Key, Value: v.Value, Set: v.Set, Source: string(v.Source), Scope: string(v.Def.Scope),
		File: fileName(cfg, v.File), Type: typ, Allowed: v.Def.Allowed, Default: v.Def.Default, Secret: v.Def.Secret,
	}
	if v.Def.Secret {
		// Only whether a token is in use, and where from; never the value.
		k.Default = nil
		k.Set = v.TokenSource != ""
		k.Value = "not set"
		if k.Set {
			k.Value = "set"
		}
		switch {
		case v.TokenSource == "":
			k.Source = ""
		case v.TokenSource == forge.StoredSource:
			k.Source = "credentials"
		case v.TokenSource == "gh" || v.TokenSource == "glab":
			k.Source = v.TokenSource
		default:
			k.Source = "env:" + v.TokenSource
		}
	}
	if v.HasOverridden {
		k.Overridden = &jsonOverridden{Source: string(config.SourceMachine), Value: v.Overridden}
	}
	return k
}

func (a App) writeConfigJSON(v *configView, vals []config.Value) int {
	out := jsonConfig{
		Machine: v.machine,
		Files: map[string]jsonConfigFile{
			"repo":        jsonFile(v.cfg.Files.Repo),
			"machine":     jsonFile(v.cfg.Files.Machine),
			"credentials": jsonFile(v.cfg.Files.Credentials),
		},
		Keys:     []jsonConfigKey{},
		Problems: []jsonProblem{},
	}
	for _, val := range vals {
		out.Keys = append(out.Keys, jsonKey(v.cfg, val))
	}
	for _, p := range v.problems {
		out.Problems = append(out.Problems, jsonProblem{File: p.File, Line: p.Line, Key: p.Key, Message: p.Message})
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(a.Stderr, "tuckerbox config:", err)
		return 1
	}
	return 0
}

// nearestKey is the known key closest to key by edit distance, or "" when
// none is close.
func nearestKey(key string, vals []config.Value) string {
	var names []string
	for _, v := range vals {
		names = append(names, v.Key)
	}
	for _, k := range config.Keys {
		if !k.Patterned() {
			names = append(names, k.Path)
		}
	}
	best, bestD := "", len(key)/2+2
	for _, n := range names {
		if d := editDistance(key, n); d < bestD {
			best, bestD = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
