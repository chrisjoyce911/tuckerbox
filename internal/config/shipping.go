package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// Shipping is repository policy; token sources are kept separately in Forges.
type Shipping struct {
	Remote, FixCI, FixReview, MergeMethod string
	MaxRounds                             int
	Archive                               bool
	Labels, ReviewFrom                    []string
	WaitTimeout                           time.Duration
}
type ForgeHost struct {
	Kind           forge.Kind `toml:"kind"`
	TokenEnv       string     `toml:"token_env"`
	BorrowCLIToken bool       `toml:"borrow_cli_token"`
	// Stored is the token from the credentials file, if it holds one for
	// this host. It is never decoded from config.toml.
	Stored forge.Token `toml:"-"`
}

func (h ForgeHost) Credentials() forge.Credentials {
	return forge.Credentials{TokenEnv: h.TokenEnv, Stored: h.Stored, BorrowCLIToken: h.BorrowCLIToken}
}

type rawShipping struct {
	Remote      *string  `toml:"remote"`
	FixCI       *string  `toml:"fix_ci"`
	FixReview   *string  `toml:"fix_review"`
	MaxRounds   *int     `toml:"max_rounds"`
	MergeMethod *string  `toml:"merge_method"`
	Archive     *bool    `toml:"archive"`
	Labels      []string `toml:"labels"`
	ReviewFrom  []string `toml:"review_from"`
	WaitTimeout *string  `toml:"wait_timeout"`
}
type rawForge struct {
	Kind           forge.Kind `toml:"kind"`
	APIURL         string     `toml:"api_url"`
	Token          any        `toml:"token"`
	TokenEnv       any        `toml:"token_env"`
	BorrowCLIToken any        `toml:"borrow_cli_token"`
}

func shippingPolicy(f *file) {
	if f.repo {
		for _, key := range []string{"token", "token_env", "borrow_cli_token"} {
			if f.defined("forge", key) {
				f.problem("forge."+key, "tokens and token sources belong in machine config ([forges.<host>])")
			}
		}
	} else {
		if f.raw.Ship != nil && *f.raw.Ship != "local" {
			f.problem("ship", "ship = %q belongs in repository config; machine config may set only local", *f.raw.Ship)
			f.raw.Ship = nil
		}
	}
}
func resolveShipping(cfg *Config, m, r *file, add addFunc) {
	cfg.Forge = forge.Settings{Kind: r.raw.Forge.Kind, APIURL: r.raw.Forge.APIURL}
	cfg.Forges = map[string]ForgeHost{}
	if m.raw.Machine != nil {
		cfg.Machine = *m.raw.Machine
		if strings.TrimSpace(cfg.Machine) == "" || strings.ContainsAny(cfg.Machine, " \t\r\n<>=") {
			add(m.path, "machine", "must be a non-empty claim marker name without whitespace, <, > or =")
		}
	}
	for host, settings := range m.raw.Forges {
		if strings.ToLower(host) != host || strings.ContainsAny(host, "/:@ \\?#") || host == "" {
			add(m.path, "forges."+host, "use a lowercase hostname without a URL or port")
		}
		if settings.Kind != "" && settings.Kind != forge.GitHub && settings.Kind != forge.GitLab {
			add(m.path, "forges."+host+".kind", "use github or gitlab")
		}
		cfg.Forges[host] = settings
	}
	if cfg.Forge.Kind != "" && cfg.Forge.Kind != forge.GitHub && cfg.Forge.Kind != forge.GitLab {
		add(r.path, "forge.kind", "use github or gitlab")
	}
	if cfg.Forge.APIURL != "" {
		u, err := url.Parse(cfg.Forge.APIURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			add(r.path, "forge.api_url", "must be an HTTPS URL without credentials, query or fragment")
		}
	}
	s := r.raw.Shipping
	cfg.Shipping = Shipping{Remote: "origin", MaxRounds: 3, MergeMethod: "merge", Archive: true, WaitTimeout: 2 * time.Hour, Labels: slices.Clone(s.Labels), ReviewFrom: slices.Clone(s.ReviewFrom)}
	for _, v := range []struct {
		key string
		src *string
		dst *string
	}{{"remote", s.Remote, &cfg.Shipping.Remote}, {"fix_ci", s.FixCI, &cfg.Shipping.FixCI}, {"fix_review", s.FixReview, &cfg.Shipping.FixReview}, {"merge_method", s.MergeMethod, &cfg.Shipping.MergeMethod}} {
		if v.src != nil {
			*v.dst = *v.src
		}
	}
	if s.FixReview == nil {
		cfg.Shipping.FixReview = cfg.Shipping.FixCI
	}
	if s.MaxRounds != nil {
		cfg.Shipping.MaxRounds = *s.MaxRounds
		if *s.MaxRounds < 1 {
			add(r.path, "shipping.max_rounds", "must be at least 1")
		}
	}
	if s.Archive != nil {
		cfg.Shipping.Archive = *s.Archive
	}
	if s.WaitTimeout != nil {
		d, err := time.ParseDuration(*s.WaitTimeout)
		if err != nil || d <= 0 {
			add(r.path, "shipping.wait_timeout", "must be a positive duration")
		}
		cfg.Shipping.WaitTimeout = d
	}
	if cfg.Shipping.Remote == "" || strings.HasPrefix(cfg.Shipping.Remote, "-") || strings.ContainsAny(cfg.Shipping.Remote, " \t\r\n") {
		add(r.path, "shipping.remote", "must name a git remote")
	}
	if !slices.Contains([]string{"merge", "squash", "rebase"}, cfg.Shipping.MergeMethod) {
		add(r.path, "shipping.merge_method", "use merge, squash or rebase")
	}
}
func resolveFixSteps(cfg *Config, root string, m, r *file, index map[string]int, known func(string) bool, add addFunc) {
	cfg.FixSteps = map[string]Step{}
	for _, pair := range []struct{ key, id string }{{"shipping.fix_ci", cfg.Shipping.FixCI}, {"shipping.fix_review", cfg.Shipping.FixReview}} {
		if pair.id == "" {
			continue
		}
		if _, ok := index[pair.id]; ok {
			add(r.path, pair.key, "step %q must not also be in workflow.steps", pair.id)
			continue
		}
		_, inRepo := r.raw.Steps[pair.id]
		_, inMachine := m.raw.Steps[pair.id]
		if !inRepo && !inMachine {
			add(r.path, pair.key, "unknown step %q", pair.id)
			continue
		}
		if _, ok := cfg.FixSteps[pair.id]; ok {
			continue
		}
		step := resolveStep(cfg, root, pair.id, 0, map[string]int{}, m.raw.Steps[pair.id], r.raw.Steps[pair.id], m.path, r.path, r.path, known, add)
		if step.OnReject != "" {
			add(r.path, "steps."+pair.id+".on_reject", "fix steps do not use workflow rejection loops")
		}
		cfg.FixSteps[pair.id] = step
	}
}

func missingRemoteError(remote string, err error) error {
	return fmt.Errorf("shipping remote %q: %w", remote, err)
}
