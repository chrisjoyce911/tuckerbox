package board

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/deps"
	"github.com/chrisjoyce911/tuckerbox/internal/edits"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

// Error provides a stable code for dashboards, including initialization errors.
type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

// Read reads a board in dir. env is the config discovery environment; a zero
// environment uses the process environment. change optionally includes detail.
func Read(ctx context.Context, dir, change string, env config.Env) (*Document, error) {
	if env.Git == nil {
		env = config.SystemEnv()
	}
	opts, err := config.Discover(env, dir)
	if err != nil {
		return nil, &Error{"not_a_repository", err.Error()}
	}
	cfg, err := config.Load(opts)
	if errors.Is(err, config.ErrNoRepoConfig) {
		return nil, &Error{"not_a_repository", err.Error()}
	}
	if err != nil {
		return nil, err
	}
	repo, err := runs.OpenRepo(ctx, opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	ids, err := (runs.OpenSpec{Dir: repo.Root}).List(ctx)
	if err != nil {
		return nil, err
	}
	key := RepoKey(ctx, opts.Remote)
	if key == "" {
		key = repo.Root
	}
	worktrees, err := scanWorktrees(ctx, repo)
	if err != nil {
		return nil, err
	}
	doc := &Document{Version: Version, Repo: key, Path: repo.Root, Machine: history.Machine(), RequireApproval: cfg.RequireApproval, Columns: columns(cfg), Cards: []Card{}}
	folders := map[string]string{}
	states := map[string]*runs.Run{}
	places := map[string]edits.Place{}
	for _, id := range ids {
		p, r, placeErr := locate(repo, cfg.Worktree, worktrees, id)
		c, err := readCard(ctx, id, p.ChangeDir(), doc.Machine, false)
		c.addError(placeErr)
		c.addError(err)
		c.EditFolder = p.Dir
		c.Column = "proposed"
		if cfg.RequireApproval && c.Approval.State == "approved" {
			c.Column = "approved"
		}
		if r != nil {
			effective := r.Effective(runs.Active(p.State.RunLock()))
			r = &effective
			states[id] = r
			c.State, c.Reason, c.Step, c.Round = string(r.State), r.Reason, r.Step, r.Round
			c.Since, c.Branch, c.Worktree = timestamp(r.Since), r.Branch, r.Worktree
			if step, ok := cfg.Step(r.Step); ok {
				c.Agent = step.Agent
			}
			switch r.State {
			case runs.Queued:
				c.Column = "queued"
			case runs.Ready:
				c.Column = "ready"
			default:
				if _, ok := cfg.Step(r.Step); ok {
					c.Column = "step:" + r.Step
				} else {
					c.Column = "queued"
					c.addError(fmt.Errorf("run step %q is absent from the workflow", r.Step))
				}
			}
		}
		places[id] = p
		folders[id] = p.ChangeDir()
		doc.Cards = append(doc.Cards, c)
	}
	archives, err := archiveFolders(repo.Root)
	if err != nil {
		return nil, err
	}
	knownColumns := map[string]string{}
	for _, a := range archives {
		knownColumns[a.id] = "done"
	}
	for i, a := range archives {
		if i >= 20 {
			break
		}
		// An active change with a reused id is the current card.
		if _, ok := folders[a.id]; ok {
			continue
		}
		c, err := readCard(ctx, a.id, a.folder, doc.Machine, true)
		c.addError(err)
		c.Column, c.Archived = "done", a.date
		folders[a.id] = a.folder
		doc.Cards = append(doc.Cards, c)
	}
	for _, c := range doc.Cards {
		knownColumns[c.Change] = c.Column
	}
	for i := range doc.Cards {
		for j := range doc.Cards[i].DependsOn {
			d := &doc.Cards[i].DependsOn[j]
			d.Column = knownColumns[d.Change]
		}
	}
	if change != "" {
		folder, ok := folders[change]
		if !ok {
			return nil, &Error{"unknown_change", "no board card for change " + change}
		}
		doc.Detail, err = readDetail(change, folder, places[change].State, states[change])
		if err != nil {
			for i := range doc.Cards {
				if doc.Cards[i].Change == change {
					doc.Cards[i].addError(err)
				}
			}
		}
	}
	return doc, nil
}

func columns(cfg *config.Config) []Column {
	out := []Column{{ID: "proposed", Kind: "proposed", Name: "Proposed"}}
	if cfg.RequireApproval {
		out = append(out, Column{ID: "approved", Kind: "approved", Name: "Approved"})
	}
	out = append(out, Column{ID: "queued", Kind: "queued", Name: "Queued"})
	for _, s := range cfg.Steps {
		r, n := utf8.DecodeRuneInString(s.ID)
		out = append(out, Column{ID: "step:" + s.ID, Kind: "step", Step: s.ID, Name: string(unicode.ToUpper(r)) + s.ID[n:]})
	}
	return append(out, Column{ID: "ready", Kind: "ready", Name: "Ready"}, Column{ID: "done", Kind: "done", Name: "Done"})
}

func readCard(ctx context.Context, id, folder, machine string, archived bool) (Card, error) {
	c := Card{Change: id, Title: strings.ReplaceAll(id, "-", " "), Machine: machine, DependsOn: []Dependency{}, Approval: Approval{State: "none", Committed: archived}}
	var sidecar struct {
		Title     string   `toml:"title"`
		DependsOn []string `toml:"depends_on"`
	}
	data, err := os.ReadFile(filepath.Join(folder, approval.SidecarName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c, err
	}
	if _, err := toml.Decode(string(data), &sidecar); err != nil {
		return c, err
	}
	if sidecar.Title != "" {
		c.Title = sidecar.Title
	}
	seen := map[string]bool{}
	for _, id := range sidecar.DependsOn {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			c.DependsOn = append(c.DependsOn, Dependency{Change: id})
			seen[id] = true
		}
	}
	tasks, err := readOptional(filepath.Join(folder, "tasks.md"))
	if err != nil {
		return c, err
	}
	for _, task := range parseTasks(tasks) {
		c.Tasks.Total++
		if task.Checked {
			c.Tasks.Done++
		}
	}
	if archived {
		return c, nil
	}
	st, err := approval.Judge(approval.Dir(folder))
	if err != nil {
		return c, err
	}
	c.Approval = Approval{State: string(st.State), Committed: true}
	if st.Approval != nil {
		c.Approval.By, c.Approval.At = st.Approval.By, timestamp(st.Approval.At)
	}
	c.Approval.Committed, err = committed(ctx, folder)
	return c, err
}

var taskLine = regexp.MustCompile(`^[ \t]*[-*+] \[([ xX])\](?:[ \t]+(?:(\d+(?:\.\d+)+)(?:[ \t]+|$))?(.*?))?[ \t]*$`)

func parseTasks(text string) []Task {
	out := []Task{}
	group := ""
	fence := ""
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		run := ""
		if len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := 0
			for n < len(trimmed) && trimmed[n] == trimmed[0] {
				n++
			}
			if n >= 3 {
				run = trimmed[:n]
			}
		}
		if fence != "" {
			if len(run) >= len(fence) && run[0] == fence[0] && strings.TrimSpace(trimmed[len(run):]) == "" {
				fence = ""
			}
			continue
		}
		if run != "" {
			fence = run
			continue
		}
		if strings.HasPrefix(line, "## ") {
			group = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
		if m := taskLine.FindStringSubmatch(line); m != nil {
			out = append(out, Task{Number: m[2], Group: group, Text: m[3], Checked: m[1] != " "})
		}
	}
	return out
}
func readOptional(p string) (string, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

type archive struct{ id, date, folder string }

var archiveName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(.+)$`)

func archiveFolders(root string) ([]archive, error) {
	entries, err := os.ReadDir(deps.ArchiveDir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := []archive{}
	for _, e := range entries {
		if m := archiveName.FindStringSubmatch(e.Name()); e.IsDir() && m != nil {
			if _, err := time.Parse("2006-01-02", m[1]); err == nil {
				out = append(out, archive{m[2], m[1], filepath.Join(deps.ArchiveDir(root), e.Name())})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].date == out[j].date {
			return out[i].id < out[j].id
		}
		return out[i].date > out[j].date
	})
	return out, nil
}

// scanWorktrees resolves each state directory once per poll, rather than once
// per card. Retain broken registrations so their own card can explain them.
type worktree struct {
	path  string
	state statedir.Dir
	run   *runs.Run
	err   error
}

func scanWorktrees(ctx context.Context, repo runs.Repo) ([]worktree, error) {
	paths, err := repo.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]worktree, 0, len(paths))
	for _, p := range paths {
		w := worktree{path: p}
		w.state, w.err = statedir.Resolve(ctx, p)
		if w.err == nil {
			w.run, w.err = runs.ReadRun(w.state.RunFile())
			if errors.Is(w.err, runs.ErrNoRun) {
				w.err = nil
			}
		}
		out = append(out, w)
	}
	return out, nil
}
func locate(repo runs.Repo, cfg config.Worktree, ws []worktree, id string) (edits.Place, *runs.Run, error) {
	p := edits.Place{Change: id, Dir: repo.Root}
	var selected *worktree
	for i := range ws {
		if ws[i].run != nil && ws[i].run.Change == id {
			selected = &ws[i]
			break
		}
	}
	if selected == nil {
		expected := repo.WorktreePath(cfg, id)
		for i := 1; i < len(ws); i++ {
			if samePath(ws[i].path, expected) {
				selected = &ws[i]
				break
			}
		}
	}
	var r *runs.Run
	if selected != nil {
		p.Dir, p.State, p.Worktree = selected.path, selected.state, true
		r = selected.run
		if selected.err != nil {
			return p, r, selected.err
		}
	}
	fi, err := os.Stat(p.ChangeDir())
	if err != nil {
		return p, r, err
	}
	if !fi.IsDir() {
		return p, r, fmt.Errorf("%s is not a change folder", p.ChangeDir())
	}
	return p, r, nil
}
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	return ea == nil && eb == nil && os.SameFile(fa, fb)
}
func (c *Card) addError(err error) {
	if err == nil {
		return
	}
	if c.Error != "" {
		c.Error += "; "
	}
	c.Error += err.Error()
}
