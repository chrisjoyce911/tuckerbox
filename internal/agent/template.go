// Package agent starts agent command-line tools for workflow steps: it fills
// in command templates, writes the step brief, runs the agent headless with
// its output in a per-step log, and reads back the result file.
//
// The package takes plain inputs (templates, resolved permission flags,
// paths) and does not read Tuckerbox config itself.
package agent

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Placeholders a command template may use.
const (
	// PhPrompt is a short instruction telling the agent to read the brief.
	PhPrompt = "{prompt}"
	// PhBrief is the absolute path of the brief file.
	PhBrief = "{brief}"
	// PhResult is the absolute path the agent writes its result to.
	PhResult = "{result}"
	// PhLastMessage is the absolute path of a file, owned by Tuckerbox, where
	// the agent CLI writes the agent's final message (codex -o). Without it,
	// the final message is read from the agent's standard output.
	PhLastMessage = "{lastmessage}"
	// PhWorkdir is the change's worktree, also the process's working directory.
	PhWorkdir = "{workdir}"
	// PhModel is the step's model; empty when the step sets none.
	PhModel = "{model}"
	// PhEffort is the step's effort; empty when the step sets none.
	PhEffort = "{effort}"
	// PhPermissions must be a whole argument. It expands to the permission
	// flags of the step's profile, or to nothing. Templates without it get the
	// flags appended at the end.
	PhPermissions = "{permissions}"
)

var known = map[string]bool{
	PhPrompt: true, PhBrief: true, PhResult: true, PhLastMessage: true, PhWorkdir: true,
	PhModel: true, PhEffort: true, PhPermissions: true,
}

var placeholderRE = regexp.MustCompile(`\{[a-z_]+\}`)

// Template is an agent definition: a command line with placeholders.
//
// An argument whose placeholders expand to an empty value (an unset model or
// effort) is left out, together with the flag just before it when that flag
// starts with "-" and has no placeholder. So ["--model", "{model}"] adds
// nothing when no model is set.
type Template struct {
	Name    string
	Command []string
}

// Builtin returns the built-in agent definitions, keyed by name.
//
// Claude Code needs --verbose for stream-json output in print mode; its
// final message is the result event at the end of that output. Codex runs
// with --no-daemon so each step's agent is its own process tree, which stop
// can end; the flag is global, so it comes before exec. Codex writes its
// final message to {lastmessage} with -o: the CLI writes that file itself,
// outside the sandbox it runs the model's commands in, so a read-only
// reviewer can still report.
func Builtin() map[string]Template {
	return map[string]Template{
		"claude": {Name: "claude", Command: []string{
			"claude", "-p", PhPrompt,
			"--output-format", "stream-json", "--verbose",
			"--model", PhModel,
			"--effort", PhEffort,
			PhPermissions,
		}},
		"codex": {Name: "codex", Command: []string{
			"codex", "--no-daemon", "exec", "--json",
			"-C", PhWorkdir,
			"-m", PhModel,
			"-c", "model_reasoning_effort=" + PhEffort,
			"-o", PhLastMessage,
			PhPermissions,
			"--", PhPrompt,
		}},
	}
}

// Resolve finds agent name, looking first in defined (agents from machine or
// repository config, which may replace a built-in) and then in the built-ins.
func Resolve(name string, defined map[string]Template) (Template, error) {
	if t, ok := defined[name]; ok {
		if t.Name == "" {
			t.Name = name
		}
		return t, nil
	}
	if t, ok := Builtin()[name]; ok {
		return t, nil
	}
	names := []string{}
	for n := range Builtin() {
		names = append(names, n)
	}
	for n := range defined {
		names = append(names, n)
	}
	sort.Strings(names)
	return Template{}, fmt.Errorf("unknown agent %q (known: %s)", name, strings.Join(names, ", "))
}

// bypassFlags are agent flags that skip or loosen permission prompts. Only a
// machine permission profile may add them, never an agent template.
var bypassFlags = []string{
	"--dangerously-skip-permissions",
	"--allow-dangerously-skip-permissions",
	"--permission-mode",
	"--permission-prompt-tool",
	"--allowedTools", "--allowed-tools",
	"--dangerously-bypass-approvals-and-sandbox",
	"--approve-for-me",
	"--full-auto",
	"--sandbox",
	"--ask-for-approval",
	"--yolo",
}

// BypassFlags returns the arguments in args that grant or change agent
// permissions. Config validation uses it to reject such flags outside machine
// permission profiles.
func BypassFlags(args []string) []string {
	var found []string
	for _, a := range args {
		flag := a
		if i := strings.IndexByte(a, '='); i > 0 && strings.HasPrefix(a, "-") {
			flag = a[:i]
		}
		for _, b := range bypassFlags {
			if flag == b {
				found = append(found, a)
				break
			}
		}
		if strings.Contains(a, "approval_policy") || strings.Contains(a, "sandbox_mode") {
			found = append(found, a)
		}
	}
	return found
}

// Validate checks a template: a program, only known placeholders,
// {permissions} only as a whole argument, and no permission flags.
func (t Template) Validate() error {
	name := t.Name
	if name == "" {
		name = "(unnamed)"
	}
	if len(t.Command) == 0 || strings.TrimSpace(t.Command[0]) == "" {
		return fmt.Errorf("agent %s: command is empty", name)
	}
	if placeholderRE.MatchString(t.Command[0]) {
		return fmt.Errorf("agent %s: the program %q cannot be a placeholder", name, t.Command[0])
	}
	for _, a := range t.Command {
		for _, ph := range placeholderRE.FindAllString(a, -1) {
			if !known[ph] {
				return fmt.Errorf("agent %s: unknown placeholder %s in %q", name, ph, a)
			}
			if ph == PhPermissions && a != PhPermissions {
				return fmt.Errorf("agent %s: %s must be a whole argument, not part of %q", name, PhPermissions, a)
			}
		}
	}
	if bad := BypassFlags(t.Command); len(bad) > 0 {
		return fmt.Errorf("agent %s: permission flags %s belong in a machine permission profile, not in the agent command",
			name, strings.Join(bad, " "))
	}
	return nil
}

// Values fills in a template's placeholders.
type Values struct {
	Prompt, Brief, Result, LastMessage, Workdir, Model, Effort string
	// Permissions are the flags of the step's machine permission profile,
	// already resolved. Nil runs the agent with its own defaults.
	Permissions []string
}

// Expand returns the command line for v. It validates the template first.
func (t Template) Expand(v Values) ([]string, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	sub := map[string]string{
		PhPrompt: v.Prompt, PhBrief: v.Brief, PhResult: v.Result, PhLastMessage: v.LastMessage,
		PhWorkdir: v.Workdir, PhModel: v.Model, PhEffort: v.Effort,
	}
	out := make([]string, 0, len(t.Command)+len(v.Permissions))
	placed := false
	for i, a := range t.Command {
		if a == PhPermissions {
			out = append(out, v.Permissions...)
			placed = true
			continue
		}
		phs := placeholderRE.FindAllString(a, -1)
		if len(phs) == 0 {
			out = append(out, a)
			continue
		}
		empty := false
		s := a
		for _, ph := range phs {
			if sub[ph] == "" {
				empty = true
			}
			s = strings.ReplaceAll(s, ph, sub[ph])
		}
		if empty {
			// Drop the argument and the flag that introduces it.
			if n := len(out); n > 1 && isBareFlag(t.Command[i-1]) && out[n-1] == t.Command[i-1] {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, s)
	}
	if !placed {
		out = append(out, v.Permissions...)
	}
	return out, nil
}

func isBareFlag(a string) bool {
	return strings.HasPrefix(a, "-") && a != "--" && !placeholderRE.MatchString(a)
}

// UsesLastMessage reports whether the template passes {lastmessage}, so the
// agent's final message is read from that file rather than its output.
func (t Template) UsesLastMessage() bool {
	for _, a := range t.Command {
		if strings.Contains(a, PhLastMessage) {
			return true
		}
	}
	return false
}
