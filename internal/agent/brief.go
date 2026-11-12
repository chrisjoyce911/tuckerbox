package agent

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed roles/*.md
var builtinRoles embed.FS

// BuiltinRoles lists the roles that have a built-in prompt.
func BuiltinRoles() []string {
	entries, _ := fs.ReadDir(builtinRoles, "roles")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

var roleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// RoleOverride is the path of a repository's prompt for role, relative to the
// repository root.
func RoleOverride(role string) string {
	return filepath.Join(".tuckerbox", "roles", role+".md")
}

// RolePrompt returns the prompt for role: the repository's
// .tuckerbox/roles/<role>.md under root if it exists, else the built-in one.
// It also returns where the prompt came from.
func RolePrompt(root, role string) (prompt, source string, err error) {
	if !roleRE.MatchString(role) {
		return "", "", fmt.Errorf("role %q: use lower-case letters, digits, '_' and '-' only", role)
	}
	rel := RoleOverride(role)
	data, err := os.ReadFile(filepath.Join(root, rel))
	switch {
	case err == nil:
		return string(data), filepath.ToSlash(rel), nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", "", fmt.Errorf("role %q: %w", role, err)
	}
	data, err = builtinRoles.ReadFile(path.Join("roles", role+".md"))
	if err != nil {
		return "", "", fmt.Errorf("role %q has no built-in prompt (built-in: %s) and %s does not exist",
			role, strings.Join(BuiltinRoles(), ", "), filepath.ToSlash(rel))
	}
	return string(data), "built-in", nil
}

// Brief is what a step's agent is told.
type Brief struct {
	Change string
	Step   string
	Role   string
	Round  int
	// Root is the change's worktree. Role overrides and change files are
	// looked up under it, and paths in the brief are relative to it.
	Root string
	// ResultPath is the absolute path the agent must write its result to.
	ResultPath string
	// Base is the ref the change's branch started from, if known.
	Base string
	// Earlier holds accepted results from earlier rounds whose findings the
	// agent must address, oldest first.
	Earlier []Result
}

// ChangeFiles returns the change's planning files, relative to root with
// forward slashes, and whether each exists.
func ChangeFiles(root, change string) (files []string, exists map[string]bool) {
	dir := path.Join("openspec", "changes", change)
	exists = map[string]bool{}
	for _, f := range []string{"proposal.md", "design.md", "tasks.md"} {
		p := path.Join(dir, f)
		files = append(files, p)
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
		exists[p] = err == nil
	}
	specs, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "specs", "*", "spec.md"))
	sort.Strings(specs)
	for _, s := range specs {
		rel, err := filepath.Rel(root, s)
		if err != nil {
			continue
		}
		p := filepath.ToSlash(rel)
		files = append(files, p)
		exists[p] = true
	}
	return files, exists
}

// Render returns the brief's text.
func (b Brief) Render() (string, error) {
	if b.Change == "" || b.Step == "" || b.Round < 1 || b.ResultPath == "" {
		return "", errors.New("brief needs a change, step, round (from 1) and result path")
	}
	prompt, source, err := RolePrompt(b.Root, b.Role)
	if err != nil {
		return "", err
	}
	var s strings.Builder
	fmt.Fprintf(&s, "# Tuckerbox brief: step %s, round %d, change %s\n\n", b.Step, b.Round, b.Change)
	s.WriteString("You are running headless: nobody will answer questions while you work. ")
	s.WriteString("Do the step below, then write the result file described at the end. ")
	s.WriteString("Tuckerbox reads only that file to decide what happens next.\n\n")

	fmt.Fprintf(&s, "## Role: %s\n\n", b.Role)
	fmt.Fprintf(&s, "<!-- role prompt: %s -->\n", source)
	s.WriteString(strings.TrimSpace(prompt))
	s.WriteString("\n\n")

	s.WriteString("## Change\n\n")
	fmt.Fprintf(&s, "- Change id: `%s`\n", b.Change)
	s.WriteString("- Working directory: this change's git worktree; paths below are relative to it.\n")
	if b.Base != "" {
		fmt.Fprintf(&s, "- Branch base: `%s`\n", b.Base)
	}
	s.WriteString("\nFiles:\n")
	files, exists := ChangeFiles(b.Root, b.Change)
	for _, f := range files {
		if exists[f] {
			fmt.Fprintf(&s, "- `%s`\n", f)
		} else {
			fmt.Fprintf(&s, "- `%s` (not written yet)\n", f)
		}
	}
	s.WriteString("\n")

	if len(b.Earlier) > 0 {
		s.WriteString("## Findings from earlier rounds\n\n")
		s.WriteString("Address every finding below.\n\n")
		for _, r := range b.Earlier {
			fmt.Fprintf(&s, "### %s, round %d: %s\n\n", r.Step, r.Round, r.Status)
			if r.Summary != "" {
				s.WriteString(strings.TrimSpace(r.Summary))
				s.WriteString("\n\n")
			}
			for _, f := range r.Findings {
				where := f.File
				if where == "" {
					where = "(whole change)"
				}
				fmt.Fprintf(&s, "- [%s] `%s`: %s\n", f.Severity, where, strings.TrimSpace(f.Detail))
			}
			if len(r.Findings) > 0 {
				s.WriteString("\n")
			}
		}
	}

	s.WriteString("## Result file\n\n")
	s.WriteString("When you finish, write this JSON object to the result file:\n\n")
	fmt.Fprintf(&s, "    %s\n\n", b.ResultPath)
	s.WriteString("```json\n")
	fmt.Fprintf(&s, "{\n  \"change\": %q,\n  \"step\": %q,\n  \"round\": %d,\n", b.Change, b.Step, b.Round)
	s.WriteString("  \"status\": \"done\",\n  \"summary\": \"One or two sentences on what you did.\",\n")
	s.WriteString("  \"findings\": [\n    {\"severity\": \"high\", \"file\": \"path/in/worktree.go\", \"detail\": \"What is wrong and how to fix it.\"}\n  ]\n}\n")
	s.WriteString("```\n\n")
	s.WriteString("Rules:\n")
	fmt.Fprintf(&s, "- `change`, `step` and `round` must be exactly %q, %q and %d. A result naming another round or step is ignored.\n",
		b.Change, b.Step, b.Round)
	s.WriteString("- `status` is one of `done`, `rejected`, `blocked`, `failed`.\n")
	s.WriteString("- `summary` is required and short.\n")
	fmt.Fprintf(&s, "- `findings` is optional. Each has `severity` (%s), `file` (relative to the worktree, or empty for the whole change) and `detail`.\n",
		strings.Join(severities, ", "))
	s.WriteString("- Write the whole file once, at the end (write a temporary file next to it and rename it). Do not write it anywhere else.\n")
	s.WriteString("- If you exit without writing a valid result file, the step is recorded as `failed`.\n")
	return s.String(), nil
}

// Write renders the brief to file p, creating its directory.
func (b Brief) Write(p string) error {
	text, err := b.Render()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(text), 0o644)
}
