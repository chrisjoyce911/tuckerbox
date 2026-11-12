package configsave

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/priority"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
)

type Problem struct {
	File    string `json:"file"`
	Key     string `json:"key"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	New     bool   `json:"new"`
}
type ChangeProblem struct {
	Change  string `json:"change"`
	Key     string `json:"key"`
	Message string `json:"message"`
}
type SaveError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *SaveError) Error() string { return e.Message }

type Result struct {
	OK       bool            `json:"ok"`
	DryRun   bool            `json:"dry_run"`
	File     string          `json:"file"`
	Target   string          `json:"target"`
	Reason   string          `json:"reason"`
	Problems []Problem       `json:"problems"`
	Changes  []ChangeProblem `json:"changes"`
	Link     string          `json:"link,omitempty"`
	Message  string          `json:"message"`
	Error    *SaveError      `json:"error"`
}

func NewResult(machine, dry bool) Result {
	f := "repo"
	if machine {
		f = "machine"
	}
	return Result{File: f, DryRun: dry, Problems: []Problem{}, Changes: []ChangeProblem{}}
}
func (r *Result) Fail(code string, err error) error {
	r.OK = false
	r.Error = &SaveError{code, err.Error()}
	return r.Error
}

// Read returns nil for an absent file, distinguishing it from an empty file.
func Read(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err == nil && b == nil {
		b = []byte{}
	}
	return b, err
}
func Match(data []byte, want string) error {
	if want == "" {
		return nil
	}
	rev := "none"
	if data != nil {
		rev = Revision(data)
	}
	if rev != want {
		return &SaveError{"changed_since_read", "config changed since read; reload and try again"}
	}
	return nil
}

type Request struct {
	Options                       config.Options
	StateDir, Path, IfMatch, Base string
	Machine, DryRun, Credentials  bool
	Edits                         []Edit
}

// Save keeps comparison, check, and rename in one OS lock. It re-reads both
// files inside the lock; it never checks a stale candidate then writes a merge.
func Save(ctx context.Context, req Request, result *Result) error {
	return WithFileLock(ctx, req.StateDir, req.Path, func() error {
		if req.Credentials {
			if st, e := os.Stat(req.Path); e == nil && st.Mode().Perm()&077 != 0 && runtime.GOOS != "windows" {
				return fmt.Errorf("%s: credentials must be private; run chmod 600 on this file", req.Path)
			}
		}
		before, err := Read(req.Path)
		if err != nil {
			return err
		}
		if err = Match(before, req.IfMatch); err != nil {
			return err
		}
		after, err := Rewrite(before, req.Edits)
		if err != nil {
			if req.Credentials {
				return fmt.Errorf("credentials file cannot be rewritten; no token was saved")
			}
			return err
		}
		if !req.Credentials {
			err = Check(req.Options, before, after, req.Machine, result)
		}
		if err != nil {
			return err
		}
		if !req.DryRun && !bytes.Equal(before, after) {
			mode := fs.FileMode(0600)
			if st, e := os.Stat(req.Path); e == nil && !req.Credentials {
				mode = st.Mode()
			}
			if err = AtomicWrite(req.Path, after, mode); err != nil {
				return err
			}
		}
		result.OK = true
		return nil
	})
}

// Check reports all remaining config diagnostics and refuses only new ones.
// Changes are advice: narrowing an override allow-list must stay possible.
func Check(opts config.Options, before, after []byte, machine bool, result *Result) error {
	repo, err := Read(opts.RepoRoot + string(os.PathSeparator) + config.RepoFile)
	if err != nil {
		return err
	}
	mach, err := Read(opts.MachinePath)
	if err != nil {
		return err
	}
	load := func(candidate []byte) (*config.Config, config.Problems, error) {
		r, m := repo, mach
		if machine {
			m = candidate
		} else {
			r = candidate
		}
		cfg, e := config.LoadData(opts, r, m)
		var ps config.Problems
		if e != nil && !errors.As(e, &ps) {
			return cfg, nil, e
		}
		return cfg, ps, nil
	}
	_, old, err := load(before)
	if err != nil {
		return err
	}
	cfg, ps, err := load(after)
	if err != nil {
		return err
	}
	oldSet := map[string]bool{}
	id := func(p config.Problem) string { return p.File + "\x00" + p.Key + "\x00" + p.Message }
	for _, p := range old {
		oldSet[id(p)] = true
	}
	added := false
	for _, p := range ps {
		file := "repo"
		if p.File == opts.MachinePath || p.File == config.CredentialsPath(opts.MachinePath) {
			file = "machine"
		}
		isNew := !oldSet[id(p)]
		added = added || isNew
		result.Problems = append(result.Problems, Problem{file, p.Key, p.Message, p.Line, isNew})
	}
	// The same repository checks as `check`, with active-change diagnostics
	// explicitly non-blocking. File access failures still fail the save.
	dp, err := deps.Check(opts.RepoRoot)
	if err != nil {
		return err
	}
	for _, p := range dp {
		result.Changes = append(result.Changes, ChangeProblem{p.Change, "depends_on", p.Msg})
	}
	pp, err := priority.Check(opts.RepoRoot)
	if err != nil {
		return err
	}
	for _, p := range pp {
		result.Changes = append(result.Changes, ChangeProblem{p.Change, "priority", p.Msg})
	}
	if len(ps) > 0 {
		cfg = nil
	}
	sp, err := settings.CheckAll(opts.RepoRoot, cfg)
	if err != nil {
		return err
	}
	for _, p := range sp {
		result.Changes = append(result.Changes, ChangeProblem{p.Change, p.Key, p.Message})
	}
	if added {
		return &SaveError{"check_failed", "save adds configuration problems"}
	}
	return nil
}
