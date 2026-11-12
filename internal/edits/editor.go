package edits

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

// ErrNoEditor means no editor is set. Its message says how to set one.
var ErrNoEditor = errors.New(`no editor is set: set editor = "<command>" in machine config, or $VISUAL or $EDITOR (a GUI editor must wait for the file to close, such as "code --wait")`)

// EditorCommand returns the editor's program and arguments: machine config's
// editor (configured) if set, else $VISUAL, else $EDITOR. A repository can't
// set the editor; config refuses it there.
func EditorCommand(configured string, getenv func(string) string) ([]string, error) {
	for _, s := range []string{configured, getenv("VISUAL"), getenv("EDITOR")} {
		if strings.TrimSpace(s) == "" {
			continue
		}
		argv, err := SplitCommand(s)
		if err != nil {
			return nil, fmt.Errorf("editor %q: %w", s, err)
		}
		return argv, nil
	}
	return nil, ErrNoEditor
}

// SplitCommand splits an editor command into words at spaces and tabs.
// Single quotes keep their text as is; double quotes keep it too, except
// that \" and \\ stand for " and \. A backslash outside quotes is a plain
// character, so Windows paths need no escaping.
func SplitCommand(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated ' quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New(`unterminated " quote`)
			}
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	if len(words) == 0 {
		return nil, errors.New("empty command")
	}
	return words, nil
}

// RunEditor runs the editor on path as a child in this process's process
// group, attached to this terminal, and waits for it. Closing the terminal
// or pane ends both. An interrupt (Ctrl-C) goes to the editor, which
// decides what to do; this process waits for it either way.
func RunEditor(argv []string, path string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(argv[0], append(argv[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %s: %w", argv[0], err)
	}
	return nil
}
