// Package doctor checks that the tools Tuckerbox runs are installed.
package doctor

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
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
}

// DefaultTools are the tools `tuckerbox doctor` checks.
var DefaultTools = []Tool{
	{Name: "git", Required: true, VersionArgs: []string{"--version"}},
	{Name: "openspec", Required: true, VersionArgs: []string{"--version"}},
	{Name: "claude", VersionArgs: []string{"--version"}, Agent: true},
	{Name: "codex", VersionArgs: []string{"--version"}, Agent: true},
}

// Result is the outcome of checking one tool.
type Result struct {
	Tool    Tool
	Found   bool
	Path    string
	Version string // empty when the version could not be read
	// Note is extra information, such as the sign-in state of an agent.
	Note string
}

// Env is how Doctor finds and runs tools. Tests replace it.
type Env struct {
	LookPath func(name string) (string, error)
	// Output runs path with args and returns its combined output.
	Output  func(ctx context.Context, path string, args ...string) ([]byte, error)
	Timeout time.Duration
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

// SignInNotChecked is reported for agent CLIs. None of them offers a sign-in
// check that is known to be free of side effects (for example, unlocking a
// keychain or refreshing a token), so doctor does not guess.
const SignInNotChecked = "sign-in not checked"

// Check checks each tool and returns one result per tool, in order.
func Check(env Env, tools []Tool) []Result {
	results := make([]Result, 0, len(tools))
	for _, t := range tools {
		r := Result{Tool: t}
		path, err := env.LookPath(t.Name)
		if err == nil {
			r.Found = true
			r.Path = path
			r.Version = readVersion(env, path, t.VersionArgs)
			if t.Agent {
				r.Note = SignInNotChecked
			}
		}
		results = append(results, r)
	}
	return results
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
			if version == "" {
				version = "version unknown"
			}
			if note != "" {
				note = "  (" + note + ")"
			}
		}
		line := r.Tool.Name + "\t" + kind + "\t" + status
		if version != "" {
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
