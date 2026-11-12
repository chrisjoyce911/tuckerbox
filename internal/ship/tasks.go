package ship

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/scope"
)

// TickShipTasks is called under the run's change lock, before archiving.
func TickShipTasks(root, change string) error {
	folder, err := changeFolder(root, change)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	path := filepath.Join(folder, "tasks.md")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text, err := scope.TickShip(string(data))
	if err != nil {
		return err
	}
	if text == string(data) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(folder, ".tasks-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		file.Close()
		return err
	}
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for i := 0; i < 20; i++ {
		err = os.Rename(name, path)
		if err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}
