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
	doc := &Document{Version: Version, Repo: key, Path: repo.Root, Machine: history.Machine(), RequireApproval: cfg.RequireApproval, Columns: columns(cfg), Cards: []Card{}}
	folders := map[string]string{}
	states := map[string]*runs.Run{}
	places := map[string]edits.Place{}
	for _, id := range ids {
		p, err := edits.LocateWith(ctx, repo.Root, id, cfg.Worktree)
		if err != nil {
			return nil, err
		}
		c, err := readCard(ctx, id, p.ChangeDir(), doc.Machine)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		c.EditFolder = p.Dir
		c.Column = "proposed"
		if cfg.RequireApproval && c.Approval.State == "approved" {
			c.Column = "approved"
		}
		if p.State != "" {
			r, err := runs.ReadRun(p.State.RunFile())
			if err != nil && !errors.Is(err, runs.ErrNoRun) {
				return nil, err
			}
			if err == nil {
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
					c.Column = "step:" + r.Step
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
		c, err := readCard(ctx, a.id, a.folder, doc.Machine)
		if err != nil {
			return nil, err
		}
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
			return nil, err
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

func readCard(ctx context.Context, id, folder, machine string) (Card, error) {
	c := Card{Change: id, Title: strings.ReplaceAll(id, "-", " "), Machine: machine, DependsOn: []Dependency{}}
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

var brisbane = time.FixedZone("AEST", 10*60*60)

func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(brisbane).Format(time.RFC3339)
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
