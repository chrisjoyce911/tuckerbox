package ship

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// archivedAlready reports whether change's folder in root is gone and the
// change is in the archive there, returning the archived folder.
func archivedAlready(root, change string) (string, bool) {
	active := filepath.Join(root, "openspec", "changes", change)
	if _, err := os.Stat(active); !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	folder, err := changeFolder(root, change)
	if err != nil {
		return "", false
	}
	return folder, true
}

// TickShipTasks is called under the run's change lock, before archiving.
func TickShipTasks(root, change string) error {
	_, err := tickTasks(root, change, func(text string) (string, []string, error) {
		out, err := scope.TickShip(text)
		return out, nil, err
	})
	return err
}

// TickCITasks ticks change's unchecked CI tasks in its tasks.md in root,
// active or archived, and returns their numbers. It is called once CI is
// green on the merge request.
func TickCITasks(root, change string) ([]string, error) {
	return tickTasks(root, change, scope.TickCI)
}

// tickCI ticks the change's CI tasks once CI is green, commits them as
// "Tick <change> task N.M" and pushes the commit. pushed reports a new
// head whose CI the caller waits for.
func (e *Engine) tickCI(ctx context.Context, claim *Claim) (pushed bool, out Outcome) {
	numbers, err := TickCITasks(e.Git.Worktree, e.Identity.Change)
	if err != nil {
		return false, e.blocked(err)
	}
	if len(numbers) == 0 {
		return false, Outcome{}
	}
	noun := "task"
	if len(numbers) > 1 {
		noun = "tasks"
	}
	made, err := e.Commit(ctx, fmt.Sprintf("Tick %s %s %s", e.Identity.Change, noun, strings.Join(numbers, ", ")))
	if err != nil {
		return false, e.blocked(err)
	}
	if !made {
		return false, Outcome{}
	}
	if err := e.checkClaim(ctx, claim); err != nil {
		if errors.Is(err, errAlreadyMerged) {
			return false, e.merged(ctx, e.Record.LastPush)
		}
		return false, e.blocked(err)
	}
	if _, err := claim.Acquire(ctx, e.Forge, e.Request, e.persistPush); err != nil {
		return false, e.blocked(err)
	}
	return true, Outcome{}
}

// tickTasks rewrites change's tasks.md in root with edit, atomically.
func tickTasks(root, change string, edit func(string) (string, []string, error)) ([]string, error) {
	folder, err := changeFolder(root, change)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	path := filepath.Join(folder, "tasks.md")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	text, numbers, err := edit(string(data))
	if err != nil {
		return nil, err
	}
	if text == string(data) {
		return numbers, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(folder, ".tasks-*")
	if err != nil {
		return nil, err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	for i := 0; i < 20; i++ {
		err = os.Rename(name, path)
		if err == nil {
			return numbers, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, err
}
