package gitlab

import (
	"context"
	"net/url"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

// FindMergedMR includes merged requests after their source branch is deleted.
func (c *Client) FindMergedMR(ctx context.Context, branch string) (*forge.MR, error) {
	var list []mergeRequest
	_, err := c.http.Do(ctx, "GET", c.project+"/merge_requests?state=merged&source_branch="+url.QueryEscape(branch)+"&per_page=100", nil, &list)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return mr(list[0]), nil
}
