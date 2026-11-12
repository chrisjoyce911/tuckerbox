package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// decoyKeys are fields the loader decodes only to refuse them with a
// clear message; they are not settings.
var decoyKeys = []string{"forge.token", "forge.token_env", "forge.borrow_cli_token"}

// loaderKeys lists every key path the loader decodes from rawFile, with
// map keys as placeholders.
func loaderKeys(t reflect.Type, prefix []string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t == reflect.TypeOf(forge.Token{}) {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			tag, _, _ := strings.Cut(t.Field(i).Tag.Get("toml"), ",")
			if tag == "" || tag == "-" {
				continue
			}
			loaderKeys(t.Field(i).Type, append(slices.Clone(prefix), tag), out)
		}
	case reflect.Map:
		if t.Elem().Kind() == reflect.Slice || t.Elem().Kind() == reflect.String {
			*out = append(*out, strings.Join(append(slices.Clone(prefix), "<x>"), "."))
			return
		}
		loaderKeys(t.Elem(), append(slices.Clone(prefix), "<x>"), out)
	default:
		*out = append(*out, strings.Join(prefix, "."))
	}
}

func samePattern(row, loader string) bool {
	a, b := strings.Split(row, "."), strings.Split(loader, ".")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if isPlaceholder(a[i]) != isPlaceholder(b[i]) || (!isPlaceholder(a[i]) && a[i] != b[i]) {
			return false
		}
	}
	return true
}

// TestKeyTableCoversLoader fails when the loader accepts a key the key
// table lacks, or the table has a row the loader does not read.
func TestKeyTableCoversLoader(t *testing.T) {
	var keys []string
	loaderKeys(reflect.TypeOf(rawFile{}), nil, &keys)
	for _, k := range keys {
		if slices.Contains(decoyKeys, k) {
			continue
		}
		if !slices.ContainsFunc(Keys, func(row Key) bool { return samePattern(row.Path, k) }) {
			t.Errorf("the loader reads %s but the key table has no row for it", k)
		}
	}
	for _, row := range Keys {
		if row.Secret {
			continue // read from the credentials file, not config.toml
		}
		if !slices.ContainsFunc(keys, func(k string) bool { return samePattern(row.Path, k) }) {
			t.Errorf("key table row %s is not read by the loader", row.Path)
		}
	}
}

func TestKeyTableRows(t *testing.T) {
	tops := map[string]Scope{}
	for _, k := range Keys {
		if s, ok := tops[k.Top()]; ok && s != k.Scope {
			t.Errorf("%s: rows under %s have different scopes; check refuses a whole top-level table", k.Path, k.Top())
		}
		tops[k.Top()] = k.Scope
		if (k.Type == TypeEnum) != (k.Allowed != nil) {
			t.Errorf("%s: enum keys, and only they, list allowed values", k.Path)
		}
		if k.Type == TypeEnum && k.Default != nil && !slices.Contains(k.Allowed, k.Default.(string)) {
			t.Errorf("%s: default %v is not allowed", k.Path, k.Default)
		}
	}
}

// sample is a value of the right type for a key, as TOML.
func sample(k Key) string {
	switch k.Type {
	case TypeInt:
		return "2"
	case TypeBool:
		return "false"
	case TypeList:
		return `["x"]`
	case TypeEnum:
		return `"` + k.Allowed[0] + `"`
	case TypeDuration:
		return `"30s"`
	}
	return `"x"`
}

// setting writes key = value as TOML, with placeholders filled in.
func setting(k Key, value string) string {
	segs := slices.Clone(k.segs)
	for i, s := range segs {
		if isPlaceholder(s) {
			segs[i] = "sample"
		}
	}
	if len(segs) == 1 {
		return segs[0] + " = " + value + "\n"
	}
	return "[" + JoinKey(segs[:len(segs)-1]) + "]\n" + segs[len(segs)-1] + " = " + value + "\n"
}

// Scenario: Scopes agree.
func TestScopesAgree(t *testing.T) {
	for _, k := range Keys {
		if k.Scope == ScopeEither || k.Secret {
			continue
		}
		t.Run(k.Path, func(t *testing.T) {
			repo, machine := minimal, ""
			wrongFile := ""
			if k.Scope == ScopeMachine {
				repo = setting(k, sample(k)) + minimal
				wrongFile = filepath.Join("", RepoFile)
			} else {
				machine = setting(k, sample(k))
				wrongFile = "config.toml"
			}
			f := newFixture(t, repo, machine)
			ps := f.problems()
			if !slices.ContainsFunc(ps, func(p Problem) bool {
				return strings.HasSuffix(p.File, wrongFile) && (p.Key == k.Path || strings.HasPrefix(k.Path, p.Key+"."))
			}) {
				t.Fatalf("no problem names %s in the wrong file: %v", k.Path, ps)
			}
		})
	}
}

func TestSplitAndJoinKey(t *testing.T) {
	for in, want := range map[string][]string{
		"max_parallel":                   {"max_parallel"},
		"steps.review.effort":            {"steps", "review", "effort"},
		`forges."git.example.org".token`: {"forges", "git.example.org", "token"},
		`forges."github.com".token_env`:  {"forges", "github.com", "token_env"},
		`"quoted"`:                       {"quoted"},
	} {
		got, err := SplitKey(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("SplitKey(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if got := JoinKey([]string{"forges", "git.example.org", "token"}); got != `forges."git.example.org".token` {
		t.Errorf("JoinKey = %s", got)
	}
	for _, bad := range []string{"", "a..b", "a.", `a."b`, "a b", "a=b"} {
		if _, err := SplitKey(bad); err == nil {
			t.Errorf("SplitKey(%q): want an error", bad)
		}
	}
	if k, ok := LookupKey(`forges."git.example.org".token`); !ok || !k.Secret {
		t.Errorf("LookupKey token = %v %v", k, ok)
	}
	if !IsTable("steps.review") || !IsTable("shipping") || IsTable("max_parallel") || IsTable("nope") {
		t.Error("IsTable")
	}
}

func mustValue(t *testing.T, cfg *Config, key string) Value {
	t.Helper()
	v, ok := cfg.Value(key)
	if !ok {
		var keys []string
		for _, v := range cfg.Values {
			keys = append(keys, v.Key)
		}
		t.Fatalf("no value for %s; have %v", key, keys)
	}
	return v
}

// Scenario: Repository value wins (sources).
func TestSourceRepoWins(t *testing.T) {
	f := newFixture(t, "max_parallel = 2\n"+minimal, "max_parallel = 4\n")
	cfg := f.mustLoad()
	v := mustValue(t, cfg, "max_parallel")
	if v.Value != int64(2) || v.Source != SourceRepo || v.File != cfg.RepoPath || !v.HasOverridden || v.Overridden != int64(4) || v.Def.Scope != ScopeEither {
		t.Fatalf("max_parallel = %+v", v)
	}
}

// Scenario: Built-in default (sources).
func TestSourceDefault(t *testing.T) {
	cfg := newFixture(t, minimal, "").mustLoad()
	v := mustValue(t, cfg, "board.columns")
	if v.Value != "compact" || v.Source != SourceDefault || v.Set || v.File != "" {
		t.Fatalf("board.columns = %+v", v)
	}
	v = mustValue(t, cfg, "steps.plan.max_rounds")
	if v.Value != int64(DefaultMaxRounds) || v.Source != SourceDefault {
		t.Fatalf("steps.plan.max_rounds = %+v", v)
	}
}

func TestSourceMachineAndSteps(t *testing.T) {
	machine := "editor = \"vim\"\n[steps.review]\neffort = \"low\"\n[permissions.edit]\nallowed_remotes = [\"*\"]\nargs.claude = [\"--x\"]\n"
	repo := minimal + "effort = \"high\"\n"
	cfg := newFixture(t, repo, machine).mustLoad()
	v := mustValue(t, cfg, "steps.review.effort")
	if v.Value != "high" || v.Source != SourceRepo || v.Overridden != "low" {
		t.Fatalf("steps.review.effort = %+v", v)
	}
	v = mustValue(t, cfg, "editor")
	if v.Value != "vim" || v.Source != SourceMachine || v.File != cfg.MachinePath {
		t.Fatalf("editor = %+v", v)
	}
	v = mustValue(t, cfg, "permissions.edit.allowed_remotes")
	if !slices.Equal(v.Value.([]string), []string{"*"}) || v.Def.Scope != ScopeMachine {
		t.Fatalf("permissions.edit.allowed_remotes = %+v", v)
	}
	mustValue(t, cfg, "permissions.edit.args.claude")
	mustValue(t, cfg, "workflow.steps")
}

func TestSourceAgentReplacedAsWhole(t *testing.T) {
	machine := "[agents.bot]\ncommand = [\"bot\", \"{brief}\"]\nmodel = \"m1\"\n"
	repo := minimal + "[agents.bot]\ncommand = [\"bot2\", \"{brief}\"]\n"
	cfg := newFixture(t, repo, machine).mustLoad()
	if v := mustValue(t, cfg, "agents.bot.model"); v.Set {
		t.Fatalf("machine model of a repository agent still shown: %+v", v)
	}
}

func TestRevision(t *testing.T) {
	f := newFixture(t, minimal, "")
	cfg := f.mustLoad()
	if cfg.Files.Repo.Revision != Revision([]byte(minimal)) || len(cfg.Files.Repo.Revision) != 64 {
		t.Fatalf("repo revision %q", cfg.Files.Repo.Revision)
	}
	if cfg.Files.Machine.Exists || cfg.Files.Machine.Revision != "" || cfg.Files.Machine.Path != f.machine {
		t.Fatalf("absent machine file: %+v", cfg.Files.Machine)
	}
}

func TestBrokenFileStillReportsOther(t *testing.T) {
	f := newFixture(t, minimal, "max_parallel = \n")
	cfg, err := f.load()
	if err == nil || cfg == nil {
		t.Fatalf("want problems and a config, got %v", err)
	}
	if cfg.Files.Machine.Error == nil || cfg.Files.Machine.Error.Line != 1 {
		t.Fatalf("machine error: %+v", cfg.Files.Machine)
	}
	if v := mustValue(t, cfg, "steps.review.on_reject"); v.Value != "build" {
		t.Fatalf("repo values missing: %+v", v)
	}
}

func TestLoadData(t *testing.T) {
	f := newFixture(t, minimal, "")
	cfg, err := LoadData(Options{RepoRoot: f.root, MachinePath: f.machine}, []byte("max_parallel = 0\n"+minimal), nil)
	ps, ok := err.(Problems)
	if !ok || len(ps) != 1 || ps[0].File != cfg.RepoPath || ps[0].Key != "max_parallel" {
		t.Fatalf("LoadData: %v", err)
	}
}

// writeCreds writes a credentials file beside the fixture's machine config.
func writeCreds(t *testing.T, f *fixture, data string, mode os.FileMode) string {
	t.Helper()
	path := CredentialsPath(f.machine)
	writeFile(t, path, data)
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// Scenario: Stored token used first.
func TestStoredTokenUsedFirst(t *testing.T) {
	f := newFixture(t, minimal, "")
	writeCreds(t, f, "[forges.\"github.com\"]\ntoken = \"stored-secret\"\n", 0o600)
	cfg := f.mustLoad()
	creds := cfg.Forges["github.com"].Credentials()
	getenv := func(name string) string {
		if name == "GITHUB_TOKEN" {
			return "env-secret"
		}
		return ""
	}
	tok, err := forge.LookupToken(context.Background(), forge.Target{Host: "github.com", Kind: forge.GitHub}, creds, getenv, nil)
	if err != nil || tok.Source != forge.StoredSource {
		t.Fatalf("token source %q, %v", tok.Source, err)
	}
	v := mustValue(t, cfg, `forges."github.com".token`)
	if v.Value != "set" || !v.Set || v.TokenSource != forge.StoredSource || v.File != CredentialsPath(f.machine) {
		t.Fatalf("token value %+v", v)
	}
	for _, v := range cfg.Values {
		if strings.Contains(strings.ToLower(strings.Join([]string{v.Key, toString(v.Value)}, " ")), "stored-secret") {
			t.Fatalf("token leaked into values: %+v", v)
		}
	}
	// token_env still comes first: an explicit choice for that host.
	creds.TokenEnv = "MY_TOKEN"
	tok, _ = forge.LookupToken(context.Background(), forge.Target{Host: "github.com", Kind: forge.GitHub}, creds,
		func(n string) string {
			if n == "MY_TOKEN" {
				return "mine"
			}
			return getenv(n)
		}, nil)
	if tok.Source != "MY_TOKEN" {
		t.Fatalf("token_env not first: %s", tok.Source)
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Scenario: Token in machine config.
func TestTokenInMachineConfig(t *testing.T) {
	f := newFixture(t, minimal, "[forges.\"github.com\"]\ntoken = \"leaky-secret\"\n")
	ps := f.problems()
	if !strings.Contains(ps.Error(), "credentials file") || !strings.Contains(ps.Error(), "tuckerbox config set --machine") {
		t.Fatalf("problems: %v", ps)
	}
	if strings.Contains(ps.Error(), "leaky-secret") {
		t.Fatal("token printed")
	}
	cfg, _ := f.load()
	for _, v := range cfg.Values {
		if toString(v.Value) == "leaky-secret" {
			t.Fatalf("token from config.toml became a value: %+v", v)
		}
	}
}

// Scenario: File readable by others.
func TestCredentialsReadableByOthers(t *testing.T) {
	if credentialsGOOS == "windows" {
		t.Skip("file modes do not apply on Windows")
	}
	f := newFixture(t, minimal, "")
	path := writeCreds(t, f, "[forges.\"github.com\"]\ntoken = \"open-secret\"\n", 0o644)
	cfg, err := f.load()
	ps, ok := err.(Problems)
	if !ok || !strings.Contains(ps.Error(), path) || !strings.Contains(ps.Error(), "chmod 600") || strings.Contains(ps.Error(), "open-secret") {
		t.Fatalf("problems: %v", err)
	}
	if cfg.Forges["github.com"].Stored.Available() {
		t.Fatal("token from an open file used")
	}
}

func TestCredentialsSyntaxErrorHidesText(t *testing.T) {
	f := newFixture(t, minimal, "")
	writeCreds(t, f, "[forges.\"github.com\"]\ntoken = \"half-secret\n", 0o600)
	_, err := f.load()
	if err == nil || strings.Contains(err.Error(), "half-secret") || !strings.Contains(err.Error(), ":2") {
		t.Fatalf("problems: %v", err)
	}
}
