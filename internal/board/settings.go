package board

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chrisjoyce911/tuckerbox/internal/approval"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/history"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/settings"
)

// cardSettings works out each workflow step's effective settings for the
// change whose folder is folder, from its tuckerbox.toml over cfg, and
// its machines. A step the run r is running shows what it started with.
// A sidecar that can't be read counts as no overrides; readCard reports
// it.
func cardSettings(cfg *config.Config, folder string, r *runs.Run) ([]StepSettings, []string) {
	sc := settings.Sidecar{Steps: map[string]settings.Override{}}
	if data, err := os.ReadFile(filepath.Join(folder, settings.FileName)); err == nil {
		if parsed, _, err := settings.Parse(data); err == nil {
			sc = parsed
		}
	}
	machines := append([]string{}, sc.Machines...)
	out := make([]StepSettings, 0, len(cfg.Steps))
	for _, st := range cfg.Steps {
		e := settings.Resolve(cfg, st, sc.Steps[st.ID])
		s := StepSettings{
			Step:        st.ID,
			Agent:       Setting{e.Agent, string(e.From.Agent)},
			Model:       Setting{e.Model, string(e.From.Model)},
			Effort:      Setting{e.Effort, string(e.From.Effort)},
			Permissions: Setting{e.Permissions, string(e.From.Permissions)},
		}
		if rec := started(r, st.ID); rec != nil {
			s = StepSettings{Step: st.ID, Agent: recorded(rec.Agent), Model: recorded(rec.Model), Effort: recorded(rec.Effort), Permissions: recorded(rec.Permissions), Started: true}
		}
		out = append(out, s)
	}
	return out, machines
}

// started returns what step started with when r is running it now, else
// nil.
func started(r *runs.Run, step string) *history.Settings {
	if r == nil || r.State != runs.Running || r.Step != step || r.StepSettings == nil {
		return nil
	}
	if r.StepSettings.Step != step || r.StepSettings.Round != r.Round {
		return nil
	}
	return &r.StepSettings.Settings
}

func recorded(s history.Setting) Setting {
	src := s.Source
	if src == "" {
		src = string(config.SourceDefault)
	}
	return Setting{s.Value, src}
}

// noChoices is a detail's choices when the repository has no [overrides].
func noChoices() Choices {
	return Choices{Agents: []string{}, Models: map[string][]string{}, Efforts: []string{}, Permissions: []string{}, VoidsApproval: []string{}}
}

// readChoices reads the [overrides] allow-list from the repository config
// in dir, the checkout or worktree where tuckerbox set edits the change and
// judges what it sets.
func readChoices(dir string) (Choices, error) {
	c := noChoices()
	data, err := os.ReadFile(filepath.Join(dir, config.RepoFile))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	o, ps := config.RepoOverrides(filepath.ToSlash(config.RepoFile), data)
	if len(ps) > 0 {
		return c, ps
	}
	if o == nil {
		return c, nil
	}
	c.Agents = append(c.Agents, o.Agents...)
	for agent, models := range o.Models {
		c.Models[agent] = append([]string{}, models...)
	}
	c.Efforts = append(c.Efforts, o.Efforts...)
	c.Permissions = append(c.Permissions, o.Permissions...)
	c.VoidsApproval = approval.VoidingKeys()
	return c, nil
}
