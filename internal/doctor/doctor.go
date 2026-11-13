// Package doctor checks that the tools Tuckerbox runs are installed.
package doctor

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

// Tool is a command-line tool Tuckerbox depends on.
type Tool struct {
	Name     string
	Required bool
	// VersionArgs are the arguments that make the tool print its version.
	VersionArgs []string
	// Agent marks agent CLIs, whose sign-in state matters for runs.
	Agent bool
	// Program is the command looked up on PATH; "" means Name. A custom
	// agent is named by its config name and found by its command.
	Program string
	// SignInArgs make an agent CLI say whether it is signed in, without
	// starting a session or using model tokens; SignInParse reads the
	// answer. An agent without them is reported as unknown.
	SignInArgs  []string
	SignInParse func(out []byte, err error) string
}

// DefaultTools are the tools `tuckerbox doctor` checks.
var DefaultTools = []Tool{
	{Name: "git", Required: true, VersionArgs: []string{"--version"}},
	{Name: "openspec", Required: true, VersionArgs: []string{"--version"}},
	{Name: "claude", VersionArgs: []string{"--version"}, Agent: true, SignInArgs: []string{"auth", "status"}, SignInParse: parseClaudeSignIn},
	{Name: "codex", VersionArgs: []string{"--version"}, Agent: true, SignInArgs: []string{"login", "status"}, SignInParse: parseCodexSignIn},
}

// Sign-in states of an agent CLI.
const (
	SignedIn  = "signed in"
	SignedOut = "signed out"
	Unknown   = "unknown"
)

// SignInTimeout is how long an agent CLI's sign-in check may take.
const SignInTimeout = 5 * time.Second

var loggedInRE = regexp.MustCompile(`"loggedIn"\s*:\s*(true|false)`)

// parseClaudeSignIn reads `claude auth status`, which prints JSON with
// loggedIn. Only that field is read: the rest (account, organisation) is
// never kept or shown.
func parseClaudeSignIn(out []byte, _ error) string {
	m := loggedInRE.FindSubmatch(out)
	switch {
	case m == nil:
		return Unknown
	case string(m[1]) == "true":
		return SignedIn
	}
	return SignedOut
}

// parseCodexSignIn reads `codex login status`: "Logged in using ..." and
// exit 0 when signed in, "Not logged in" otherwise.
func parseCodexSignIn(out []byte, err error) string {
	s := strings.ToLower(string(out))
	switch {
	case strings.Contains(s, "not logged in"):
		return SignedOut
	case err == nil && strings.Contains(s, "logged in"):
		return SignedIn
	}
	return Unknown
}

// Result is the outcome of checking one tool.
type Result struct {
	Tool    Tool
	Found   bool
	Path    string
	Version string // empty when the version could not be read
	// Note is extra information, such as the sign-in state of an agent.
	Note string
	// SignIn is an agent's sign-in state: SignedIn, SignedOut or Unknown;
	// "" for other tools and agents not found.
	SignIn string
}

// Env is how Doctor finds and runs tools. Tests replace it.
type Env struct {
	LookPath func(name string) (string, error)
	// Output runs path with args and returns its combined output.
	Output  func(ctx context.Context, path string, args ...string) ([]byte, error)
	Timeout time.Duration
	// SignInTimeout limits each sign-in check; 0 means [SignInTimeout].
	SignInTimeout time.Duration
}

// SystemEnv finds tools on PATH and runs them for real.
func SystemEnv() Env {
	return Env{
		LookPath: exec.LookPath,
		Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, path, args...).CombinedOutput()
		},
		Timeout: 10 * time.Second,
	}
}

// Check checks each tool and returns one result per tool, in order.
func Check(env Env, tools []Tool) []Result {
	results := make([]Result, 0, len(tools))
	for _, t := range tools {
		r := Result{Tool: t}
		program := t.Program
		if program == "" {
			program = t.Name
		}
		path, err := env.LookPath(program)
		if err == nil {
			r.Found = true
			r.Path = path
			if len(t.VersionArgs) > 0 {
				r.Version = readVersion(env, path, t.VersionArgs)
			}
			if t.Agent {
				r.SignIn = SignIn(env, path, t)
				r.Note = r.SignIn
			}
		}
		results = append(results, r)
	}
	return results
}

// SignIn asks the agent CLI at path whether it is signed in, with the
// tool's own status command and a [SignInTimeout]. It never signs in or
// changes agent config. An agent without a known check is Unknown.
func SignIn(env Env, path string, t Tool) string {
	if len(t.SignInArgs) == 0 || t.SignInParse == nil {
		return Unknown
	}
	timeout := env.SignInTimeout
	if timeout <= 0 {
		timeout = SignInTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := env.Output(ctx, path, t.SignInArgs...)
	if ctx.Err() != nil {
		return Unknown
	}
	return t.SignInParse(out, err)
}

// AgentTools returns the tools doctor checks for agents: the built-in
// ones and each custom agent, named by its config name and found by its
// command's program.
func AgentTools(custom map[string]string) []Tool {
	var out []Tool
	for _, t := range DefaultTools {
		if t.Agent {
			out = append(out, t)
		}
	}
	for _, name := range sortedKeys(custom) {
		if builtin(name) {
			continue
		}
		out = append(out, Tool{Name: name, Program: custom[name], Agent: true})
	}
	return out
}

func builtin(name string) bool {
	for _, t := range DefaultTools {
		if t.Agent && t.Name == name {
			return true
		}
	}
	return false
}

func readVersion(env Env, path string, args []string) string {
	timeout := env.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := env.Output(ctx, path, args...)
	if err != nil {
		return ""
	}
	return ParseVersion(string(out))
}

var versionRE = regexp.MustCompile(`\bv?\d+\.\d+(?:\.\d+)?(?:[-+.][0-9A-Za-z.-]+)?`)

// ParseVersion picks the version number out of a tool's version output, such
// as "git version 2.43.0" or "1.0.33 (Claude Code)". It falls back to the
// first non-empty line, and returns "" for empty output.
func ParseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if v := versionRE.FindString(line); v != "" {
			return v
		}
		return line
	}
	return ""
}

// OK reports whether every required tool was found.
func OK(results []Result) bool {
	for _, r := range results {
		if r.Tool.Required && !r.Found {
			return false
		}
	}
	return true
}

// Write prints the results as a table, followed by a summary line.
func Write(w io.Writer, results []Result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range results {
		kind := "optional"
		if r.Tool.Required {
			kind = "required"
		}
		status, version, note := "missing", "", r.Note
		if r.Found {
			status = "found"
			version = r.Version
			if version == "" && len(r.Tool.VersionArgs) > 0 {
				version = "version unknown"
			}
			if note != "" {
				note = "  (" + note + ")"
				if version == "" {
					note = "(" + r.Note + ")"
				}
			}
		}
		line := r.Tool.Name + "\t" + kind + "\t" + status
		if version != "" || note != "" {
			line += "\t" + version + note
		}
		fmt.Fprintln(tw, line)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	var missing []string
	for _, r := range results {
		if r.Tool.Required && !r.Found {
			missing = append(missing, r.Tool.Name)
		}
	}
	if len(missing) > 0 {
		_, err := fmt.Fprintf(w, "\nmissing required tools: %s\n", strings.Join(missing, ", "))
		return err
	}
	_, err := fmt.Fprintln(w, "\nall required tools found")
	return err
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
