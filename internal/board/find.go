package board

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

// Find returns this machine's checkout of the repository whose board key is
// key (a normalised remote URL, as Document.Repo holds). It looks in dir's
// repository first, then in every repository the machine index in stateHome
// knows. A board on another machine names the repository by key only, so
// this is how each machine finds its own copy (herdr-board: Runs from every
// machine). A repository with no remote has its path as its key and is found
// only by that path. It returns an Error with code repo_not_found when this
// machine has no checkout of the repository.
func Find(ctx context.Context, key, dir, stateHome string, env config.Env) (string, error) {
	if env.Git == nil {
		env = config.SystemEnv()
	}
	var tried []string
	seen := map[string]bool{}
	try := func(folder string) (string, bool) {
		root, err := config.RepoRoot(env, folder)
		if err != nil {
			return "", false
		}
		repo, err := runs.OpenRepo(ctx, root)
		if err != nil {
			return "", false
		}
		if seen[repo.Main] {
			return "", false
		}
		seen[repo.Main] = true
		tried = append(tried, repo.Main)
		k := RepoKey(ctx, config.Remote(env, repo.Main))
		if k == "" {
			k = repo.Main
		}
		if k == key || (filepath.IsAbs(key) && sameDir(key, repo.Main)) {
			return repo.Main, true
		}
		return "", false
	}
	if dir != "" {
		if m, ok := try(dir); ok {
			return m, nil
		}
	}
	if stateHome != "" {
		ix, err := runs.ReadIndex(stateHome)
		if err == nil {
			for _, e := range ix.Runs {
				if _, err := os.Stat(e.Repo); err != nil {
					continue
				}
				if m, ok := try(e.Repo); ok {
					return m, nil
				}
			}
		}
	}
	return "", &Error{"repo_not_found", fmt.Sprintf("no checkout of %s on this machine (looked in %d known repositories)", key, len(tried))}
}

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ia, err1 := os.Stat(a)
	ib, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}
