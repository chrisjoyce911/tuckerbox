// Package version reports the build's version information. The values are
// set at build time with -ldflags, for example:
//
//	go build -ldflags "-X github.com/chrisjoyce911/tuckerbox/internal/version.Version=v0.1.0 \
//	  -X github.com/chrisjoyce911/tuckerbox/internal/version.Commit=$(git rev-parse --short HEAD)" \
//	  ./cmd/tuckerbox
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set with -ldflags "-X". They stay "dev" and "unknown" for a plain go build.
var (
	Version = "dev"
	Commit  = "unknown"
)

// Info is the version information of the running binary.
type Info struct {
	Version   string
	Commit    string
	GoVersion string
	OS        string
	Arch      string
}

// Get returns the running binary's version information. When Commit was not
// set with -ldflags, it falls back to the VCS revision Go embeds in the build.
func Get() Info {
	commit := Commit
	if commit == "unknown" {
		if rev := vcsRevision(); rev != "" {
			commit = rev
		}
	}
	return Info{
		Version:   Version,
		Commit:    commit,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

func vcsRevision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev != "" && modified == "true" {
		rev += "-dirty"
	}
	return rev
}

// String formats the information as the `tuckerbox version` output.
func (i Info) String() string {
	return fmt.Sprintf("tuckerbox %s\ncommit: %s\ngo: %s\nplatform: %s/%s\n",
		i.Version, i.Commit, i.GoVersion, i.OS, i.Arch)
}
