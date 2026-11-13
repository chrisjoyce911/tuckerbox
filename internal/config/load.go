package config

import (
	"context"
	"errors"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Problem is one thing wrong with the configuration. Every problem names the
// file it is in and, when it is about a setting, the dotted key.
type Problem struct {
	File    string // config file, as given to Load
	Line    int    // line in File for syntax and type errors; 0 otherwise (the TOML decoder gives no line for unknown keys)
	Key     string // dotted key, such as "steps.review.on_reject"; may be "" for syntax errors
	Message string
}

func (p Problem) String() string {
	var b strings.Builder
	b.WriteString(p.File)
	if p.Line > 0 {
		fmt.Fprintf(&b, ":%d", p.Line)
	}
	b.WriteString(": ")
	if p.Key != "" {
		b.WriteString(p.Key)
		b.WriteString(": ")
	}
	b.WriteString(p.Message)
	return b.String()
}

// Problems is the error [Load] returns when the configuration is invalid.
type Problems []Problem

func (ps Problems) Error() string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

// ErrNoRepoConfig is returned (wrapped) by [Load] when the repository has no
// .tuckerbox/config.toml. Its message says how to create one.
var ErrNoRepoConfig = errors.New("no " + filepath.ToSlash(RepoFile) + " in this repository; run `tuckerbox init` to create one")

// Options says what [Load] reads. [Discover] fills it in for a directory.
type Options struct {
	// RepoRoot is the repository root; the repository config is
	// RepoRoot/.tuckerbox/config.toml.
	RepoRoot string
	// MachinePath is the machine config file. It need not exist; "" skips it.
	MachinePath string
	// Remote is the repository's remote URL (any form git accepts), or ""
	// if it has none. Permission profiles are matched against it.
	Remote string
	// PushURL returns the effective push URL of the selected shipping remote.
	PushURL func(remote string) (string, error)
}

// Load reads the machine and repository config, merges them with repository
// precedence, fills in defaults and validates the result.
//
// It returns an error wrapping [ErrNoRepoConfig] when the repository config
// is missing, a [Problems] error listing every problem found when the config
// is invalid, or another error when a file cannot be read. The returned
// Config is non-nil whenever the files could be read, even with problems.
func Load(opts Options) (*Config, error) {
	repoPath := filepath.Join(opts.RepoRoot, RepoFile)
	repoData, err := os.ReadFile(repoPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", opts.RepoRoot, ErrNoRepoConfig)
	}
	if err != nil {
		return nil, err
	}
	var machineData []byte
	if opts.MachinePath != "" {
		data, err := os.ReadFile(opts.MachinePath)
		switch {
		case err == nil:
			machineData = data
			if machineData == nil {
				machineData = []byte{}
			}
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
	}
	return LoadData(opts, repoData, machineData)
}

// LoadData is [Load] on file contents given in memory, as a save would
// leave them: repoData is the repository config's text and machineData
// the machine config's, or nil when the machine file is absent. Problems
// name the files' real paths. The credentials file is read from disk.
func LoadData(opts Options, repoData, machineData []byte) (*Config, error) {
	repoPath := filepath.Join(opts.RepoRoot, RepoFile)
	if repoData == nil {
		return nil, fmt.Errorf("%s: %w", opts.RepoRoot, ErrNoRepoConfig)
	}
	var machine *file
	machinePath := ""
	if opts.MachinePath != "" && machineData != nil {
		machinePath = opts.MachinePath
		machine = parse(machinePath, string(machineData), false)
	}
	repo := parse(repoPath, string(repoData), true)
	creds, err := readCredentials(CredentialsPath(opts.MachinePath))
	if err != nil {
		return nil, err
	}
	files := Files{
		Repo:        fileInfo(repo, repoData),
		Credentials: creds.info,
	}
	if machine != nil {
		files.Machine = fileInfo(machine, machineData)
	} else {
		files.Machine = FileInfo{Path: opts.MachinePath}
	}
	if repo.broken || (machine != nil && machine.broken) {
		// Checks on a half-read file only add noise; report what stopped
		// decoding.
		var ps Problems
		if machine != nil {
			ps = append(ps, machine.problems...)
		}
		ps = append(ps, repo.problems...)
		ps = append(ps, creds.problems...)
		// Report what the files that did decode set.
		var mv, rv *file
		if machine != nil && !machine.broken {
			mv = machine
		}
		if !repo.broken {
			rv = repo
		}
		return &Config{RepoRoot: opts.RepoRoot, RepoPath: repoPath, MachinePath: machinePath,
			Files: files, Values: values(mv, rv, creds, nil)}, ps
	}

	cfg, problems := resolve(opts.RepoRoot, opts.Remote, machine, repo)
	cfg.RepoPath = repoPath
	cfg.MachinePath = machinePath
	cfg.Files = files
	problems = append(problems, creds.problems...)
	for host, tok := range creds.tokens {
		h := cfg.Forges[host]
		h.Stored = forge.StoredToken(tok)
		cfg.Forges[host] = h
	}
	workflow := make([]string, len(cfg.Steps))
	for i, s := range cfg.Steps {
		workflow[i] = s.ID
	}
	cfg.Values = values(machine, repo, creds, workflow)
	if len(problems) == 0 && cfg.Mode == ModeFleet {
		if p, ok := fleetRemoteProblem(opts, cfg, repoPath); !ok {
			problems = append(problems, p)
		}
	}
	if len(problems) == 0 && cfg.Ship != ShipLocal {
		remote := opts.Remote
		var lookupErr error
		if opts.PushURL != nil {
			remote, lookupErr = opts.PushURL(cfg.Shipping.Remote)
		}
		var target forge.Target
		var detectErr error
		if lookupErr == nil {
			key := gitremote.Key(context.Background(), remote)
			host, _, _ := strings.Cut(key, "/")
			target, detectErr = forge.DetectKey(key, cfg.Forge, cfg.Forges[host].Kind)
		} else {
			detectErr = missingRemoteError(cfg.Shipping.Remote, lookupErr)
		}
		if detectErr != nil {
			problems = append(problems, Problem{File: repoPath, Key: "forge", Message: detectErr.Error()})
		} else {
			cfg.ForgeTarget = target
		}
	}
	if len(problems) > 0 {
		return cfg, problems
	}
	return cfg, nil
}

func fileInfo(f *file, data []byte) FileInfo {
	info := FileInfo{Path: f.path, Exists: true, Revision: Revision(data)}
	if f.broken && len(f.problems) > 0 {
		p := f.problems[0]
		info.Error = &p
	}
	return info
}

// file is one parsed config file.
type file struct {
	path     string
	repo     bool
	raw      rawFile
	tree     map[string]any // the file as decoded TOML, for sources
	defined  func(key ...string) bool
	problems Problems
	broken   bool // the file could not be decoded; its other checks are skipped
}

func (f *file) problem(key, format string, args ...any) {
	f.problems = append(f.problems, Problem{File: f.path, Key: key, Message: fmt.Sprintf(format, args...)})
}

// rawFile is the TOML shape of both config files. Machine-only keys are
// decoded from either file so that a repository setting them gets a clear
// message rather than "unknown key".
type rawFile struct {
	Machine     *string              `toml:"machine"`
	Shipping    rawShipping          `toml:"shipping"`
	Forge       rawForge             `toml:"forge"`
	Forges      map[string]ForgeHost `toml:"forges"`
	MaxParallel *int                 `toml:"max_parallel"`
	Ship        *string              `toml:"ship"`
	Editor      *string              `toml:"editor"`
	// AgentBackend is machine-only; it is decoded from repository config
	// too, so that setting it there gets a clear message.
	AgentBackend *string `toml:"agent_backend"`
	// RequireApproval is repository-only; it is decoded from machine
	// config too, so that setting it there gets a clear message.
	RequireApproval *bool                 `toml:"require_approval"`
	Workflow        rawWorkflow           `toml:"workflow"`
	Steps           map[string]rawStep    `toml:"steps"`
	Agents          map[string]rawAgent   `toml:"agents"`
	Permissions     map[string]rawProfile `toml:"permissions"`
	Board           rawBoard              `toml:"board"`
	Worktree        rawWorktree           `toml:"worktree"`
	Autopilot       rawAutopilot          `toml:"autopilot"`
	// Overrides is repository-only; it is decoded from machine config
	// too, so that setting it there gets a clear message.
	Overrides *rawOverrides `toml:"overrides"`
	// Mode, Fleet and Team are repository-only; FleetCapacity and
	// Machines are machine-only. Each is decoded from both files, so that
	// setting it in the wrong one gets a clear message.
	Mode          *string               `toml:"mode"`
	Fleet         rawFleet              `toml:"fleet"`
	Team          rawTeam               `toml:"team"`
	FleetCapacity *int                  `toml:"fleet_capacity"`
	Machines      map[string]rawMachine `toml:"machines"`
}

type rawFleet struct {
	MaxRuns *int    `toml:"max_runs"`
	Lease   *string `toml:"lease"`
}

type rawTeam struct {
	Approvers []string `toml:"approvers"`
}

type rawMachine struct {
	SSH *string `toml:"ssh"`
	HC  *string `toml:"hc"`
}

type rawOverrides struct {
	Agents      []string            `toml:"agents"`
	Models      map[string][]string `toml:"models"`
	Efforts     []string            `toml:"efforts"`
	Permissions []string            `toml:"permissions"`
}

type rawAutopilot struct {
	Scope    *string `toml:"scope"`
	Interval *string `toml:"interval"`
}

type rawBoard struct {
	Columns *string `toml:"columns"`
}

type rawWorkflow struct {
	Steps []string `toml:"steps"`
}

type rawStep struct {
	Agent       *string `toml:"agent"`
	Role        *string `toml:"role"`
	Permissions *string `toml:"permissions"`
	Model       *string `toml:"model"`
	Effort      *string `toml:"effort"`
	OnReject    *string `toml:"on_reject"`
	MaxRounds   *int    `toml:"max_rounds"`
}

type rawAgent struct {
	Command     []string `toml:"command"`
	Interactive []string `toml:"interactive"`
	Model       string   `toml:"model"`
	Effort      string   `toml:"effort"`
}

type rawProfile struct {
	Args           map[string][]string `toml:"args"`
	AllowedRemotes []string            `toml:"allowed_remotes"`
}

type rawWorktree struct {
	Base   *string `toml:"base"`
	Path   *string `toml:"path"`
	Branch *string `toml:"branch"`
}

// IsArgKey reports whether a key name carries agent arguments or
// permission settings, which only a machine permission profile may grant.
func IsArgKey(name string) bool { return slices.Contains(rawArgKeys, name) }

// rawArgKeys are key names that carry agent arguments or permission
// settings. Only a machine permission profile may grant those.
var rawArgKeys = []string{
	"args", "extra_args", "flags", "arguments",
	"permission_mode", "permission-mode", "sandbox", "approval", "approval_policy", "ask_for_approval",
	"dangerously_skip_permissions", "yolo", "full_auto",
}

// decodeErrRE matches the decoder's errors for values of the wrong type,
// which BurntSushi/toml reports as plain errors rather than ParseError.
var decodeErrRE = regexp.MustCompile(`^toml: (?:line (\d+) )?\(last key "([^"]*)"\): (.*)$`)

func parse(path, data string, repo bool) *file {
	f := &file{path: path, repo: repo, defined: func(...string) bool { return false }}
	md, err := toml.Decode(data, &f.raw)
	if err != nil {
		var pe toml.ParseError
		if errors.As(err, &pe) {
			f.problems = append(f.problems, Problem{File: path, Line: pe.Position.Line, Key: pe.LastKey, Message: pe.Message})
		} else if m := decodeErrRE.FindStringSubmatch(err.Error()); m != nil {
			line, _ := strconv.Atoi(m[1])
			f.problems = append(f.problems, Problem{File: path, Line: line, Key: m[2], Message: m[3]})
		} else {
			f.problems = append(f.problems, Problem{File: path, Message: err.Error()})
		}
		f.broken = true
		return f
	}
	f.defined = md.IsDefined
	// The same text as a plain tree, for reporting each key's value and
	// source. It decoded above, so it decodes here.
	_, _ = toml.Decode(data, &f.tree)
	shippingPolicy(f)

	undecoded := map[string]bool{}
	for _, k := range md.Undecoded() {
		undecoded[k.String()] = true
	}
	for _, k := range md.Undecoded() {
		if parentUndecoded(k, undecoded) {
			continue // reported with its table
		}
		name := k[len(k)-1]
		switch {
		case !repo && name == "token" && len(k) == 3 && k[0] == "forges":
			// Never quote the value: it is a token.
			f.problem(k.String(), "tokens belong in the credentials file, not config.toml; remove this line and run `tuckerbox config set --machine %s` to store it", JoinKey(k))
		case name == "token" || name == "token_env" || name == "borrow_cli_token":
			f.problem(k.String(), "tokens and token sources belong in machine config ([forges.<host>])")
		case slices.Contains(rawArgKeys, name):
			if repo {
				f.problem(k.String(), "agent arguments and permission settings are not allowed in repository config; they belong in a permission profile in machine config")
			} else {
				f.problem(k.String(), "unknown key; agent arguments belong in a permission profile ([permissions.<name>.args])")
			}
		default:
			f.problem(k.String(), "unknown key")
		}
	}

	checkScopes(f, md)
	return f
}

// checkScopes refuses every top-level key the key table gives the other
// file's scope, and drops its value so nothing later uses it.
func checkScopes(f *file, md toml.MetaData) {
	tops := scopeTops()
	for _, top := range slices.Sorted(maps.Keys(tops)) {
		scope := tops[top]
		wrong := (f.repo && scope == ScopeMachine) || (!f.repo && scope == ScopeRepo)
		if !wrong || !md.IsDefined(top) {
			continue
		}
		if leafTops[top] {
			// Name each key set, such as fleet.max_runs, not only the table.
			named := false
			for _, k := range md.Keys() {
				if len(k) == 2 && k[0] == top {
					f.problem(k.String(), "%s", WrongFileMessage(top, scope))
					named = true
				}
			}
			if !named {
				f.problem(top, "%s", WrongFileMessage(top, scope))
			}
		} else {
			f.problem(top, "%s", WrongFileMessage(top, scope))
		}
		clearTop(&f.raw, top)
		delete(f.tree, top)
	}
}

// leafTops are the tables whose wrong-file problems name each key set in
// them, so `[fleet] max_runs` in machine config says fleet.max_runs.
var leafTops = map[string]bool{"fleet": true, "team": true}

// clearTop zeroes the field of raw decoded from the top-level key top.
func clearTop(raw *rawFile, top string) {
	v := reflect.ValueOf(raw).Elem()
	for i := 0; i < v.NumField(); i++ {
		if tag, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("toml"), ","); tag == top {
			v.Field(i).SetZero()
			return
		}
	}
}

func parentUndecoded(k toml.Key, undecoded map[string]bool) bool {
	for i := 1; i < len(k); i++ {
		if undecoded[k[:i].String()] {
			return true
		}
	}
	return false
}
