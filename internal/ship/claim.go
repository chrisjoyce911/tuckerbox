package ship

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type Identity struct{ Change, Machine, Run string }

func MachineName(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	return os.Hostname()
}
func (i Identity) Marker() (string, error) {
	for _, v := range []string{i.Change, i.Machine, i.Run} {
		if v == "" || strings.ContainsAny(v, " \t\r\n<>=\x00") {
			return "", fmt.Errorf("invalid claim identity")
		}
	}
	return fmt.Sprintf("<!-- tuckerbox: change=%s machine=%s run=%s -->", i.Change, i.Machine, i.Run), nil
}

var markerRE = regexp.MustCompile(`<!--\s*tuckerbox:\s*(.*?)\s*-->`)

func ParseMarker(body string) (Identity, bool) {
	matches := markerRE.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		return Identity{}, false
	}
	fields := strings.Fields(matches[0][1])
	if len(fields) != 3 {
		return Identity{}, false
	}
	values := map[string]string{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || v == "" || values[k] != "" {
			return Identity{}, false
		}
		values[k] = v
	}
	i := Identity{Change: values["change"], Machine: values["machine"], Run: values["run"]}
	if _, err := i.Marker(); err != nil {
		return Identity{}, false
	}
	return i, true
}

// ClaimForge is the only forge surface needed to claim a change. Duplicate
// opens must be recovered by reading the one open MR and checking its marker.
type ClaimForge interface {
	FindMR(context.Context, string) (*forge.MR, error)
	OpenMR(context.Context, forge.OpenRequest) (*forge.MR, error)
}
type Claim struct {
	Identity Identity
	Git      Git
	Lease    string
	MR       *forge.MR
}
type SaveLease func(string) error

func (c *Claim) owner(mr *forge.MR) error {
	id, ok := ParseMarker(mr.Body)
	if !ok {
		return fmt.Errorf("claimed by another owner: %s", mr.URL)
	}
	if id != c.Identity {
		return fmt.Errorf("claimed by %s: %s", id.Machine, mr.URL)
	}
	return nil
}
func (c *Claim) read(ctx context.Context, f ClaimForge) (*forge.MR, string, error) {
	mr, err := f.FindMR(ctx, c.Git.Branch)
	if err != nil {
		return nil, "", err
	}
	if mr != nil {
		if err := c.owner(mr); err != nil {
			return mr, "", err
		}
	}
	tip, err := c.Git.Tip(ctx)
	return mr, tip, err
}

// Acquire does not push at all until foreign claims and leftover refs have
// been refused. The lease is saved immediately after a successful push,
// before opening the MR, so a crash between those operations is resumable.
func (c *Claim) Acquire(ctx context.Context, f ClaimForge, request forge.OpenRequest, save SaveLease) (*forge.MR, error) {
	if save == nil {
		return nil, fmt.Errorf("shipping requires durable lease storage")
	}
	marker, err := c.Identity.Marker()
	if err != nil {
		return nil, err
	}
	mr, tip, err := c.read(ctx, f)
	if err != nil {
		return nil, err
	}
	if c.Lease == "" && tip != "" {
		return nil, fmt.Errorf("remote branch exists")
	}
	if tip != c.Lease {
		return nil, fmt.Errorf("branch changed on the forge")
	}
	head, err := c.Git.Head(ctx)
	if err != nil {
		return nil, err
	}
	if head != c.Lease {
		pushed, err := c.Git.Push(ctx, c.Lease)
		if err != nil {
			return nil, err
		}
		c.Lease = pushed
		if err := save(pushed); err != nil {
			return nil, fmt.Errorf("saving push lease: %w", err)
		}
	}
	if mr != nil {
		c.MR = mr
		return mr, nil
	}
	request.Branch = c.Git.Branch
	request.Base = strings.TrimPrefix(c.Git.Base, c.Git.remote()+"/")
	request.Body = strings.TrimSpace(request.Body) + "\n\n" + marker
	mr, err = f.OpenMR(ctx, request)
	if err != nil {
		e, ok := err.(*forge.APIError)
		if !ok || (e.Status != 409 && e.Status != 422) {
			return nil, err
		}
		mr, err = f.FindMR(ctx, c.Git.Branch)
		if err != nil {
			return nil, err
		}
		if mr == nil {
			return nil, fmt.Errorf("forge refused open but no merge request was found")
		}
	}
	if mr == nil {
		return nil, fmt.Errorf("forge returned no merge request")
	}
	if err := c.owner(mr); err != nil {
		return nil, err
	}
	c.MR = mr
	return mr, nil
}

// Check fences every later push, reply and merge against both claim and tip.
func (c *Claim) Check(ctx context.Context, f ClaimForge) error {
	mr, tip, err := c.read(ctx, f)
	if err != nil {
		return err
	}
	if mr == nil {
		return fmt.Errorf("merge request closed")
	}
	if tip != c.Lease || (mr.Head != "" && mr.Head != c.Lease) {
		return fmt.Errorf("branch changed on the forge")
	}
	c.MR = mr
	return nil
}
func (c *Claim) Resume(ctx context.Context, f ClaimForge, request forge.OpenRequest, save SaveLease) (*forge.MR, error) {
	if save == nil {
		return nil, fmt.Errorf("shipping requires durable lease storage")
	}
	mr, tip, err := c.read(ctx, f)
	if err != nil {
		return nil, err
	}
	if tip != c.Lease {
		if c.Lease == "" {
			return nil, fmt.Errorf("remote branch exists")
		}
		if mr == nil {
			return nil, fmt.Errorf("branch changed on the forge")
		}
		adopted, err := c.Git.Adopt(ctx, c.Lease)
		if err != nil {
			return nil, err
		}
		if save == nil {
			return nil, fmt.Errorf("shipping requires durable lease storage")
		}
		c.Lease = adopted
		if err := save(adopted); err != nil {
			return nil, err
		}
	}
	return c.Acquire(ctx, f, request, save)
}
