package config

import (
	"context"
	"errors"
	"fmt"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
	"io/fs"
	"os"
	"path/filepath"
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

	var machine *file
	machinePath := ""
	if opts.MachinePath != "" {
		data, err := os.ReadFile(opts.MachinePath)
		switch {
		case err == nil:
			machinePath = opts.MachinePath
			machine = parse(machinePath, string(data), false)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, err
		}
	}
	repo := parse(repoPath, string(repoData), true)
	if repo.broken || (machine != nil && machine.broken) {
		// Checks on a half-read file only add noise; report what stopped
		// decoding.
		var ps Problems
		if machine != nil {
			ps = append(ps, machine.problems...)
		}
		ps = append(ps, repo.problems...)
		return &Config{RepoRoot: opts.RepoRoot, RepoPath: repoPath, MachinePath: machinePath}, ps
	}

	cfg, problems := resolve(opts.RepoRoot, opts.Remote, machine, repo)
	cfg.RepoPath = repoPath
	cfg.MachinePath = machinePath
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

// file is one parsed config file.
type file struct {
	path     string
	repo     bool
	raw      rawFile
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

	if repo {
		if md.IsDefined("permissions") {
			f.problem("permissions", "permission profiles are machine-only; define them in machine config and name them from a step with permissions = \"<profile>\"")
			f.raw.Permissions = nil
		}
		if md.IsDefined("editor") {
			f.problem("editor", "editor is machine-only; set it in machine config")
			f.raw.Editor = nil
		}
		if md.IsDefined("agent_backend") {
			f.problem("agent_backend", "agent_backend belongs in machine config: whether herdr-core is installed and running is a fact about the machine")
			f.raw.AgentBackend = nil
		}
	} else if md.IsDefined("require_approval") {
		f.problem("require_approval", "require_approval belongs in repository config (.tuckerbox/config.toml), so every machine applies the same gate")
		f.raw.RequireApproval = nil
	}
	return f
}

func parentUndecoded(k toml.Key, undecoded map[string]bool) bool {
	for i := 1; i < len(k); i++ {
		if undecoded[k[:i].String()] {
			return true
		}
	}
	return false
}
