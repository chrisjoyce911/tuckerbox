package edits

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// taskLine matches a task line in tasks.md: "- [ ] 2.1 …", with "-", "*" or
// "+" and any indentation. Group 1 is the checkbox character, group 2 the
// task number.
var taskLine = regexp.MustCompile(`^[ \t]*[-*+] \[([ xX])\] (\d+(?:\.\d+)+)(?:[ \t]|\r?$)`)

// ErrNoTask means tasks.md has no task with the number given.
type ErrNoTask struct{ Number, Path string }

func (e *ErrNoTask) Error() string {
	return fmt.Sprintf("no task %s exists in %s", e.Number, e.Path)
}

var taskNumber = regexp.MustCompile(`^\d+(?:\.\d+)+$`)

// SetTask sets the checkbox of task number n in the tasks.md at path to
// checked or unchecked. Only that one byte changes: line endings and every
// other byte stay as they were. It reports whether the file changed: a task
// already in that state leaves the file alone.
//
// It refuses an unknown task number, and a number that appears on more than
// one task line. Task lines inside fenced code blocks are not tasks.
func SetTask(path, n string, checked bool) (changed bool, err error) {
	if !taskNumber.MatchString(n) {
		return false, fmt.Errorf("task number %q: use the task's number, such as 2.1", n)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	at, err := findTask(data, n)
	if err != nil {
		if e, ok := err.(*ErrNoTask); ok {
			e.Path = path
		}
		return false, err
	}
	want := byte(' ')
	if checked {
		want = 'x'
	}
	if cur := data[at]; (cur == ' ') == !checked {
		return false, nil
	}
	data[at] = want
	return true, writeKeepingMode(path, data)
}

// findTask returns the offset of task n's checkbox character.
func findTask(data []byte, n string) (int, error) {
	found, count := -1, 0
	var fence []byte // the open fence's run of ` or ~, or nil
	off := 0
	for off < len(data) {
		end := bytes.IndexByte(data[off:], '\n')
		line := data[off:]
		if end >= 0 {
			line = data[off : off+end]
		}
		run, rest := fenceRun(line)
		switch {
		case fence == nil && run != nil:
			fence = run
		case fence != nil:
			// A closing fence: the same character, at least as long, and
			// nothing after it but spaces.
			if run != nil && run[0] == fence[0] && len(run) >= len(fence) && len(bytes.TrimSpace(rest)) == 0 {
				fence = nil
			}
		default:
			if m := taskLine.FindSubmatchIndex(line); m != nil && string(line[m[4]:m[5]]) == n {
				found = off + m[2]
				count++
			}
		}
		if end < 0 {
			break
		}
		off += end + 1
	}
	switch count {
	case 0:
		return 0, &ErrNoTask{Number: n}
	case 1:
		return found, nil
	}
	return 0, fmt.Errorf("task %s appears %d times in tasks.md; fix the numbering first", n, count)
}

// fenceRun returns the run of three or more ` or ~ a code fence line
// starts with (after indentation), and the rest of the line; nil when the
// line is not a fence.
func fenceRun(line []byte) (run, rest []byte) {
	t := bytes.TrimLeft(line, " \t")
	if len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return nil, nil
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return nil, nil
	}
	return t[:n], t[n:]
}

// writeKeepingMode replaces path with data through a temporary file and a
// rename, keeping its permissions.
func writeKeepingMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tuckerbox-"+strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	var err error
	for i := 0; i < 50; i++ {
		// On Windows the rename fails while another process has path open.
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	os.Remove(tmp)
	return err
}
