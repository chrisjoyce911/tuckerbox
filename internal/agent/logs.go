package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// ErrNoLog means the step has no log file yet.
var ErrNoLog = errors.New("no log for this step yet")

// openLog opens a step's log for appending, creating it and its directory.
func openLog(p string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// ReadLog copies the whole log at p to w.
func ReadLog(p string, w io.Writer) error {
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: %w", p, ErrNoLog)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// TailLog returns at most the last n bytes of the log at p.
func TailLog(p string, n int64) (string, error) {
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s: %w", p, ErrNoLog)
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	off := fi.Size() - n
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, n))
	return string(data), err
}

// FollowOptions control FollowLog.
type FollowOptions struct {
	// Poll is how often the file is checked for new output (default 200ms).
	Poll time.Duration
	// Done, if set, is called after each poll that found no new output; when
	// it returns true, FollowLog drains what is left and returns. The runs
	// code passes "the step is no longer running".
	Done func() bool
	// WaitForFile makes FollowLog wait for the log to appear instead of
	// returning ErrNoLog.
	WaitForFile bool
}

// FollowLog copies the log at p to w and then keeps copying what is appended,
// like tail -f, until ctx ends or opts.Done reports true. A log that shrinks
// (replaced or truncated) is read again from the start.
func FollowLog(ctx context.Context, p string, w io.Writer, opts FollowOptions) error {
	poll := opts.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	var f *os.File
	for f == nil {
		var err error
		f, err = os.Open(p)
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist) && opts.WaitForFile:
			if opts.Done != nil && opts.Done() {
				return fmt.Errorf("%s: %w", p, ErrNoLog)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
		case errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s: %w", p, ErrNoLog)
		default:
			return err
		}
	}
	defer func() { f.Close() }()

	var off int64
	buf := make([]byte, 32<<10)
	for {
		// Drain what is there.
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return werr
				}
				off += int64(n)
			}
			if err == io.EOF || n == 0 {
				break
			}
			if err != nil {
				return err
			}
		}
		if opts.Done != nil && opts.Done() {
			// One last drain for output written just before the step ended.
			if _, err := io.Copy(w, f); err != nil {
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
		if fi, err := os.Stat(p); err == nil && fi.Size() < off {
			nf, err := os.Open(p)
			if err != nil {
				return err
			}
			f.Close()
			f, off = nf, 0
		}
	}
}
