package github

import (
	"context"
	"net/url"
	"strconv"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// FindMergedMR includes closed pull requests, even after their branch is deleted.
func (c *Client) FindMergedMR(ctx context.Context, branch string) (*forge.MR, error) {
	for page := 1; ; page++ {
		var list []struct {
			pull
			MergedAt *string `json:"merged_at"`
		}
		h, err := c.http.Do(ctx, "GET", c.repo+"/pulls?state=closed&head="+url.QueryEscape(c.owner+":"+branch)+"&per_page=100&page="+strconv.Itoa(page), nil, &list)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			if p.MergedAt != nil || p.Merged {
				m := mr(p.pull)
				m.State = "merged"
				return m, nil
			}
		}
		if !forge.NextPage(h, page) {
			return nil, nil
		}
	}
}
