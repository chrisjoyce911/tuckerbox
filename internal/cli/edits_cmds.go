package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrisjoyce911/tuckerbox/internal/edits"
)

// notCommitted is said after every change edit, tasks, approve and
// unapprove make: they never commit.
const notCommitted = "not committed: commit and push it yourself"

// editPlace finds where change is edited, from the working directory.
func (a App) editPlace(cmd, change string) (edits.Place, bool) {
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		a.errf(cmd, "%v", err)
		return edits.Place{}, false
	}
	p, err := edits.Locate(context.Background(), wd, change)
	if err != nil {
		a.errf(cmd, "%v", err)
		return p, false
	}
	return p, true
}

// edit: tuckerbox edit <change> proposal|design|model|tasks|spec:<capability>
func (a App) edit(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 2 {
		return a.usageErr("edit", "usage: tuckerbox edit <change> proposal|design|model|tasks|spec:<capability>")
	}
	file, err := edits.ParseFile(pos[1])
	if err != nil {
		return a.usageErr("edit", err.Error())
	}
	_, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return a.errf("edit", "%v", err)
	}
	_, cfg, ok := a.loadRepo("edit", wd)
	if !ok {
		return 1
	}
	env, _ := a.configEnv()
	argv, err := edits.EditorCommand(cfg.Editor, env.Getenv)
	if err != nil {
		return a.errf("edit", "%v", err)
	}
	p, ok := a.editPlace("edit", pos[0])
	if !ok {
		return 1
	}
	path, err := file.Path(p)
	if err != nil {
		return a.errf("edit", "%v", err)
	}
	ctx := context.Background()
	var editorErr error
	err = edits.WithChangeLock(ctx, p, "tuckerbox edit "+file.String(), func() error {
		fmt.Fprintf(a.Stdout, "editing %s in %s\n", filepath.ToSlash(filepath.Join("openspec", "changes", p.Change, file.Rel())), p.Describe())
		editorErr = edits.RunEditor(argv, path, os.Stdin, a.Stdout, a.Stderr)
		return nil
	})
	if err != nil {
		return a.errf("edit", "%v", err)
	}
	code := 0
	if editorErr != nil {
		a.errf("edit", "%v", editorErr)
		code = 1
	}
	res, err := edits.Validator{}.Validate(ctx, p)
	if err != nil {
		return a.errf("edit", "%v", err)
	}
	fmt.Fprint(a.Stdout, res.Text())
	fmt.Fprintf(a.Stdout, "%s in %s\n", notCommitted, p.Dir)
	if !res.Valid {
		code = 1
	}
	return code
}

// validate: tuckerbox validate <change> [--json]
func (a App) validate(args []string) int {
	var asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, nil)
	if err != nil || len(pos) != 1 {
		return a.usageErr("validate", "usage: tuckerbox validate <change> [--json]")
	}
	p, ok := a.editPlace("validate", pos[0])
	if !ok {
		return 1
	}
	res, err := edits.Validator{}.Validate(context.Background(), p)
	if err != nil {
		return a.errf("validate", "%v", err)
	}
	if asJSON {
		if code := a.writeJSON(res); code != 0 {
			return code
		}
	} else {
		fmt.Fprint(a.Stdout, res.Text())
	}
	if !res.Valid {
		return 1
	}
	return 0
}

// tasks: tuckerbox tasks <change> tick|untick <n.m>
func (a App) tasks(args []string) int {
	pos, err := parseArgs(args, nil, nil)
	if err != nil || len(pos) != 3 || (pos[1] != "tick" && pos[1] != "untick") {
		return a.usageErr("tasks", "usage: tuckerbox tasks <change> tick|untick <n.m>")
	}
	tick, n := pos[1] == "tick", pos[2]
	p, ok := a.editPlace("tasks", pos[0])
	if !ok {
		return 1
	}
	path := filepath.Join(p.ChangeDir(), "tasks.md")
	var changed bool
	err = edits.WithChangeLock(context.Background(), p, "tuckerbox tasks "+pos[1]+" "+n, func() error {
		var err error
		changed, err = edits.SetTask(path, n, tick)
		return err
	})
	var noTask *edits.ErrNoTask
	switch {
	case errors.As(err, &noTask):
		return a.errf("tasks", "change %s: no task %s exists in tasks.md (in %s)", p.Change, n, p.Describe())
	case err != nil:
		return a.errf("tasks", "%v", err)
	}
	done := map[bool]string{true: "ticked", false: "unticked"}[tick]
	if !changed {
		fmt.Fprintf(a.Stdout, "task %s of %s is already %s; nothing changed in %s\n", n, p.Change, done, p.Describe())
		return 0
	}
	fmt.Fprintf(a.Stdout, "%s task %s of %s in %s\n%s\n", done, n, p.Change, p.Describe(), notCommitted)
	return 0
}
