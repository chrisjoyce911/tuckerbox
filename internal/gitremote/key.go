package gitremote

import (
	"context"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Key normalizes clone URLs and resolves SSH Host aliases locally through
// ssh -G. It never connects to the remote. If ssh is unavailable, the literal
// host remains the best available identity.
func Key(ctx context.Context, remote string) string {
	return Resolve(ctx, remote, func(ctx context.Context, host string) (string, error) {
		c := exec.CommandContext(ctx, "ssh", "-G", "--", host)
		c.WaitDelay = 500 * time.Millisecond
		c.Stderr = io.Discard
		configureSSH(c)
		out, err := c.Output()
		return string(out), err
	})
}

// Resolve normalizes a remote using a local SSH config query. The callback must not connect to the host.
var sshHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Resolve(ctx context.Context, remote string, ssh func(context.Context, string) (string, error)) string {
	key := Normalize(remote)
	var host string
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil && (u.Scheme == "ssh" || u.Scheme == "git+ssh") {
			host = u.Hostname()
		}
	} else if i := strings.Index(remote, ":"); i > 1 && !strings.ContainsAny(remote[:i], `/\`) {
		host = remote[:i]
		if j := strings.LastIndex(host, "@"); j >= 0 {
			host = host[j+1:]
		}
	}
	if !sshHost.MatchString(host) {
		return key
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := ssh(ctx, host)
	if err != nil {
		return key
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.EqualFold(f[0], "hostname") {
			if _, rest, ok := strings.Cut(key, "/"); ok {
				return strings.ToLower(f[1]) + "/" + rest
			}
		}
	}
	return key
}
