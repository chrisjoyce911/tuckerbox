package forge

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type Credentials struct {
	TokenEnv       string
	BorrowCLIToken bool
}

// Token deliberately has no exported secret field and is safe to format.
type Token struct {
	value  string
	Source string
}

func (t Token) String() string   { return t.Source }
func (t Token) GoString() string { return "forge.Token{Source:" + fmt.Sprintf("%q", t.Source) + "}" }
func (t Token) Available() bool  { return t.value != "" }

// ReadToken only reads environment or explicitly enabled CLI storage. It never
// mutates the process environment, so borrowed tokens cannot reach child agents.
func ReadToken(ctx context.Context, target Target, cfg Credentials) (Token, error) {
	return LookupToken(ctx, target, cfg, os.Getenv, borrowToken)
}
func LookupToken(ctx context.Context, target Target, cfg Credentials, getenv func(string) string, borrow func(context.Context, string, ...string) (string, error)) (Token, error) {
	names := []string{}
	if cfg.TokenEnv != "" {
		names = append(names, cfg.TokenEnv)
	}
	switch target.Host {
	case "github.com":
		names = append(names, "GH_TOKEN", "GITHUB_TOKEN")
	case "gitlab.com":
		names = append(names, "GITLAB_TOKEN")
	}
	for _, name := range names {
		if v := strings.TrimSpace(getenv(name)); v != "" {
			return Token{v, name}, nil
		}
	}
	if cfg.BorrowCLIToken {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var program string
		var args []string
		switch target.Kind {
		case GitHub:
			program = "gh"
			args = []string{"auth", "token", "--hostname", target.Host}
		case GitLab:
			program = "glab"
			args = []string{"config", "get", "token", "--host", target.Host}
		default:
			return Token{}, fmt.Errorf("unknown forge kind")
		}
		out, err := borrow(ctx, program, args...)
		if err == nil && strings.TrimSpace(out) != "" {
			return Token{strings.TrimSpace(out), program}, nil
		}
		// CLI stderr/errors can contain credentials. Never expose them.
		return Token{}, fmt.Errorf("no token for %s; check %s or machine [forges.%q] token_env", target.Host, program, target.Host)
	}
	return Token{}, nil
}
func borrowToken(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	return string(out), err
}

// Masker masks all tokens held by a caller, longest first (including overlaps).
type Masker struct{ tokens []string }

func NewMasker(tokens ...Token) Masker {
	m := Masker{}
	for _, t := range tokens {
		if t.Available() {
			m.tokens = append(m.tokens, t.value)
		}
	}
	sort.Slice(m.tokens, func(i, j int) bool { return len(m.tokens[i]) > len(m.tokens[j]) })
	return m
}
func (m Masker) Text(s string) string {
	for _, t := range m.tokens {
		s = strings.ReplaceAll(s, t, "***")
	}
	return s
}
