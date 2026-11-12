package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/configsave"
	"github.com/chrisjoyce911/tuckerbox/internal/edits"
	"github.com/chrisjoyce911/tuckerbox/internal/forge"
	"github.com/chrisjoyce911/tuckerbox/internal/gitremote"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
	"github.com/chrisjoyce911/tuckerbox/internal/ship"
	"golang.org/x/term"
)

type configSetFlags struct {
	machine, local, pr, dry, json bool
	match                         string
	set, unset                    []string
}

// Unlike parseArgs, unset is repeatable. Never include the argument in a
// parser error: a mistaken token=value must not leak through JSON or stderr.
func configSetArgs(args []string) (f configSetFlags, err error) {
	for i := 0; i < len(args); i++ {
		s := args[i]
		switch s {
		case "--machine":
			f.machine = true
		case "--local":
			f.local = true
		case "--pr":
			f.pr = true
		case "--dry-run":
			f.dry = true
		case "--json":
			f.json = true
		case "--unset", "--if-match":
			if i+1 == len(args) {
				return f, fmt.Errorf("%s needs a value", s)
			}
			i++
			if s == "--unset" {
				f.unset = append(f.unset, args[i])
			} else {
				f.match = args[i]
			}
		default:
			if strings.HasPrefix(s, "--unset=") {
				f.unset = append(f.unset, strings.TrimPrefix(s, "--unset="))
			} else if strings.HasPrefix(s, "--if-match=") {
				f.match = strings.TrimPrefix(s, "--if-match=")
			} else if strings.HasPrefix(s, "-") {
				return f, fmt.Errorf("unknown config set flag")
			} else {
				f.set = append(f.set, s)
			}
		}
	}
	if f.local && f.pr {
		return f, fmt.Errorf("choose only one of --local and --pr")
	}
	if f.machine && (f.local || f.pr) {
		return f, fmt.Errorf("--local and --pr are repository save options")
	}
	if len(f.set)+len(f.unset) == 0 {
		return f, fmt.Errorf("provide key=value or --unset key")
	}
	return f, nil
}
func configValue(k config.Key, s string) (any, error) {
	switch k.Type {
	case config.TypeInt:
		v, e := strconv.ParseInt(s, 10, 64)
		if e != nil {
			return nil, fmt.Errorf("%s: expected integer", k.Path)
		}
		return v, nil
	case config.TypeBool:
		if s != "true" && s != "false" {
			return nil, fmt.Errorf("%s: expected true or false", k.Path)
		}
		return s == "true", nil
	case config.TypeList:
		if s == "" {
			return []string{}, nil
		}
		v := strings.Split(s, ",")
		for i := range v {
			v[i] = strings.TrimSpace(v[i])
		}
		return v, nil
	default:
		if len(k.Allowed) > 0 && !slices.Contains(k.Allowed, s) {
			return nil, fmt.Errorf("%s: allowed values: %s", k.Path, strings.Join(k.Allowed, ", "))
		}
		return s, nil
	}
}
func configKey(s string, unset bool) (config.Key, []string, error) {
	p, err := config.SplitKey(s)
	if err != nil {
		return config.Key{}, nil, fmt.Errorf("invalid config key")
	}
	// Accept the token command's customary unquoted host spelling too;
	// storage always quotes the complete hostname as one TOML component.
	if len(p) > 3 && p[0] == "forges" && p[len(p)-1] == "token" {
		p = []string{"forges", strings.Join(p[1:len(p)-1], "."), "token"}
	}
	canonical := config.JoinKey(p)
	k, ok := config.LookupKey(canonical)
	if ok {
		return k, p, nil
	}
	if unset {
		for _, row := range config.Keys {
			parts := strings.Split(row.Path, ".")
			if len(p) >= len(parts) {
				continue
			}
			matched := true
			for i, seg := range p {
				if !strings.HasPrefix(parts[i], "<") && parts[i] != seg {
					matched = false
					break
				}
			}
			if matched {
				return row, p, nil
			}
		}
	}
	return k, nil, fmt.Errorf("unknown config key %q", canonical)
}

func (a App) configSet(args []string) int {
	f, parseErr := configSetArgs(args)
	// Honour --json even when it follows an invalid argument.
	f.json = slices.Contains(args, "--json")
	res := configsave.NewResult(f.machine, f.dry)
	if f.machine {
		res.Target = "machine"
	} else if f.pr {
		res.Target = "pr"
	} else if f.local {
		res.Target = "local"
	}
	finish := func(err error) int {
		if err != nil {
			res.Message = ""
			code := "failed"
			var se *configsave.SaveError
			if errors.As(err, &se) {
				code = se.Code
			}
			res.Fail(code, err)
		}
		if f.json {
			a.writeJSON(res)
		} else {
			for _, p := range res.Problems {
				fmt.Fprintf(a.Stderr, "%s: %s: %s\n", p.File, p.Key, p.Message)
			}
			for _, p := range res.Changes {
				fmt.Fprintf(a.Stderr, "change %s: %s: %s\n", p.Change, p.Key, p.Message)
			}
			if err == nil {
				fmt.Fprintf(a.Stdout, "%s (%s: %s)\n", res.Message, res.Target, res.Reason)
			}
		}
		if err != nil {
			return a.errf("config set", "%v", err)
		}
		return 0
	}
	if parseErr != nil {
		return finish(parseErr)
	}
	var editsList []configsave.Edit
	secretCount := 0
	pendingTokens := []int{}
	add := func(arg string, unset bool) error {
		key, value, hasValue := strings.Cut(arg, "=")
		if unset {
			key = arg
			hasValue = false
		}
		k, p, err := configKey(key, unset)
		if err != nil {
			return err
		}
		if (k.Scope == config.ScopeMachine && !f.machine) || (k.Scope == config.ScopeRepo && f.machine) {
			where := "repository"
			if k.Scope == config.ScopeMachine {
				where = "machine"
			}
			return &configsave.SaveError{Code: "wrong_file", Message: config.JoinKey(p) + " belongs in " + where + " config"}
		}
		e := configsave.Edit{Path: p, Unset: unset}
		if k.Secret {
			if len(p) != 3 || p[0] != "forges" || p[2] != "token" || p[1] == "" || strings.ToLower(p[1]) != p[1] || strings.ContainsAny(p[1], "/:@ \\?#\t\r\n") {
				return fmt.Errorf("forge token host must be a lowercase hostname without a URL or port")
			}
			secretCount++
			if hasValue {
				return fmt.Errorf("leave out the token value and enter it at the prompt or through standard input")
			}
			if !unset {
				pendingTokens = append(pendingTokens, len(editsList))
			}
		} else if !unset {
			if !hasValue {
				return fmt.Errorf("config set requires key=value")
			}
			e.Value, err = configValue(k, value)
			if err != nil {
				return err
			}
		}
		for _, prev := range editsList {
			if slices.Equal(prev.Path, p) {
				return fmt.Errorf("a key may appear only once in a save")
			}
		}
		editsList = append(editsList, e)
		return nil
	}
	for _, s := range f.set {
		if err := add(s, false); err != nil {
			return finish(err)
		}
	}
	for _, s := range f.unset {
		if err := add(s, true); err != nil {
			return finish(err)
		}
	}
	if secretCount > 0 && secretCount != len(editsList) {
		return finish(fmt.Errorf("save tokens separately from machine config keys"))
	}
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return finish(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	repo, err := runs.OpenRepo(ctx, wd)
	if err != nil {
		return finish(err)
	}
	opts, err := config.Discover(env, repo.Checkout())
	if err != nil {
		return finish(err)
	}
	stateDir, err := a.stateHome()
	if err != nil {
		return finish(err)
	}
	req := configsave.Request{Options: opts, StateDir: stateDir, IfMatch: f.match, Machine: f.machine, DryRun: f.dry, Credentials: secretCount > 0, Edits: editsList}
	req.Path = filepath.Join(opts.RepoRoot, config.RepoFile)
	if f.machine {
		req.Path = opts.MachinePath
		res.Target = "machine"
		res.Reason = "this machine only; never committed or shared"
		if req.Credentials {
			req.Path = config.CredentialsPath(opts.MachinePath)
		}
		// Validate every argument before consuming any token input.
		input := a.Stdin
		if input == nil {
			input = os.Stdin
		}
		reader := bufio.NewReader(input)
		for _, i := range pendingTokens {
			v, err := a.configToken(input, reader)
			if err != nil {
				return finish(err)
			}
			req.Edits[i].Value = v
		}
		err = configsave.Save(ctx, req, &res)
		res.Message = "saved on this machine; nothing was committed"
		if req.Credentials {
			res.Message = "token saved"
		}
		if f.dry {
			res.Message = "checked; would save on this machine"
		}
		return finish(err)
	}
	if f.local {
		res.Target = "local"
		res.Reason = "--local requested"
	} else {
		cfg, loadErr := config.Load(opts)
		if cfg == nil {
			return finish(loadErr)
		}
		remote := cfg.Shipping.Remote
		if remote == "" {
			remote = "origin"
		}
		pushURL, e := opts.PushURL(remote)
		key := gitremote.Key(ctx, pushURL)
		host, _, _ := strings.Cut(key, "/")
		target, detectErr := forge.DetectKey(key, cfg.Forge, cfg.Forges[host].Kind)
		if e != nil || detectErr != nil {
			if f.pr {
				return finish(fmt.Errorf("cannot open config pull request: forge remote is unavailable"))
			}
			res.Target = "local"
			res.Reason = "no supported forge remote on this machine"
		} else {
			borrow := a.BorrowToken
			if borrow == nil {
				borrow = forge.BorrowCLIToken
			}
			token, tokenErr := forge.LookupToken(ctx, target, cfg.Forges[host].Credentials(), env.Getenv, borrow)
			if tokenErr != nil && !f.pr {
				res.Target = "local"
				res.Reason = "no token for " + host + " on this machine"
			} else if tokenErr != nil {
				return finish(tokenErr)
			} else if !token.Available() {
				if f.pr {
					return finish(fmt.Errorf("no token for %s on this machine; --pr cannot save locally", host))
				}
				res.Target = "local"
				res.Reason = "no token for " + host + " on this machine"
			} else {
				req.Base = cfg.Worktree.Base
				cfg.ForgeTarget = target
				res.Target = "pr"
				res.Reason = "forge token available for " + host
				connect := func() (forge.Client, func(string) string, error) {
					if a.Forge != nil {
						return a.Forge(ctx, cfg)
					}
					return ship.Connect(ctx, cfg, nil)
				}
				client, mask, err := connect()
				if err != nil {
					return finish(err)
				}
				err = configsave.PullRequest(ctx, req, remote, client, mask, &res)
				return finish(err)
			}
		}
	}
	err = configsave.Save(ctx, req, &res)
	res.Message = "saved to the main checkout; nothing was committed. Commit and push for other machines and new runs; runs in existing worktrees keep their own copy."
	if f.dry {
		res.Message = "checked; would save to the main checkout without committing"
	}
	return finish(err)
}
func (a App) configToken(input io.Reader, reader *bufio.Reader) (string, error) {
	var b []byte
	if f, ok := input.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(a.Stderr, "Forge token: ")
		var err error
		b, err = term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(a.Stderr)
		if err != nil {
			return "", fmt.Errorf("could not read token")
		}
	} else {
		s, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("could not read token")
		}
		b = []byte(strings.TrimRight(s, "\r\n"))
	}
	if len(b) == 0 {
		return "", fmt.Errorf("token is empty")
	}
	return string(b), nil
}

func (a App) configEdit(args []string) int {
	var machine, asJSON bool
	pos, err := parseArgs(args, map[string]*bool{"machine": &machine, "json": &asJSON}, nil)
	if err != nil || len(pos) > 0 {
		return a.errf("config edit", "use config edit [--machine] [--json]")
	}
	res := configsave.NewResult(machine, false)
	res.Target = "local"
	if machine {
		res.Target = "machine"
	}
	finish := func(err error) int {
		if err != nil {
			res.Fail("failed", err)
		} else {
			res.OK = true
		}
		if asJSON {
			a.writeJSON(res)
		}
		if err != nil {
			return a.errf("config edit", "%v", err)
		}
		return 0
	}
	env, getwd := a.configEnv()
	wd, err := getwd()
	if err != nil {
		return finish(err)
	}
	repo, err := runs.OpenRepo(context.Background(), wd)
	if err != nil {
		return finish(err)
	}
	opts, err := config.Discover(env, repo.Checkout())
	if err != nil {
		return finish(err)
	}
	cfg, _ := config.Load(opts)
	editor := ""
	if cfg != nil {
		editor = cfg.Editor
	}
	if editor == "" {
		// The repository may be broken; still honour the machine's editor.
		if data, e := configsave.Read(opts.MachinePath); e == nil {
			var raw struct {
				Editor string `toml:"editor"`
			}
			if _, e = toml.Decode(string(data), &raw); e == nil {
				editor = raw.Editor
			}
		}
	}
	argv, err := edits.EditorCommand(editor, env.Getenv)
	if err != nil {
		return finish(err)
	}
	path := filepath.Join(opts.RepoRoot, config.RepoFile)
	if machine {
		path = opts.MachinePath
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return finish(err)
	}
	input := a.Stdin
	if input == nil {
		input = os.Stdin
	}
	editorOut := a.Stdout
	if asJSON {
		editorOut = a.Stderr
	}
	if err = edits.RunEditor(argv, path, input, editorOut, a.Stderr); err != nil {
		return finish(err)
	}
	_, err = config.Load(opts)
	var ps config.Problems
	if errors.As(err, &ps) {
		for _, p := range ps {
			file := "repo"
			if p.File == opts.MachinePath {
				file = "machine"
			}
			res.Problems = append(res.Problems, configsave.Problem{File: file, Key: p.Key, Message: p.Message, Line: p.Line})
			fmt.Fprintln(a.Stderr, p)
		}
	}
	if err != nil {
		return finish(err)
	}
	// check also reports dependency, priority and change-setting problems.
	checkApp := a
	if asJSON {
		checkApp.Stdout = io.Discard
	}
	checkApp.Getwd = func() (string, error) { return opts.RepoRoot, nil }
	if checkApp.check() != 0 {
		return finish(fmt.Errorf("config check failed after editing"))
	}
	res.Message = "config ok; nothing was committed"
	return finish(nil)
}
