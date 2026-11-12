package github

import (
	"context"
	"fmt"

	"github.com/chrisjoyce911/tuckerbox/internal/forge"
)

type pageInfo struct {
	HasNextPage bool
	EndCursor   string
}
type gqlComment struct {
	ID, Body, URL, AuthorAssociation string
	Author                           struct{ Login string }
}
type gqlThread struct {
	ID, Path   string
	Line       int
	IsResolved bool
	Comments   struct {
		Nodes    []gqlComment
		PageInfo pageInfo
	}
}
type graphqlResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes    []gqlThread
					PageInfo pageInfo
				}
			}
		}
		Node struct {
			Comments struct {
				Nodes    []gqlComment
				PageInfo pageInfo
			}
		}
	}
	Errors []struct{ Message string }
}

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any) (graphqlResponse, error) {
	var r graphqlResponse
	// GitHub Enterprise GraphQL is /api/graphql, rather than /api/v3/graphql.
	// HTTP endpoint paths are relative to its base, so transport supplies GraphQL separately.
	_, err := c.http.GraphQL(ctx, map[string]any{"query": query, "variables": variables}, &r)
	if err == nil && len(r.Errors) > 0 {
		err = fmt.Errorf("GitHub GraphQL: %s", r.Errors[0].Message)
	}
	return r, err
}
func comment(c gqlComment) forge.Comment {
	writer := c.AuthorAssociation == "OWNER" || c.AuthorAssociation == "MEMBER" || c.AuthorAssociation == "COLLABORATOR"
	return forge.Comment{ID: c.ID, Text: c.Body, URL: c.URL, Author: c.Author.Login, Writer: writer}
}
func (c *Client) Threads(ctx context.Context, n int) ([]forge.Thread, error) {
	const query = `query($owner:String!,$repo:String!,$number:Int!,$after:String){repository(owner:$owner,name:$repo){pullRequest(number:$number){reviewThreads(first:100,after:$after){nodes{id path line isResolved comments(first:100){nodes{id body url author{login} authorAssociation} pageInfo{hasNextPage endCursor}}}pageInfo{hasNextPage endCursor}}}}}`
	var out []forge.Thread
	var after any
	for {
		r, err := c.graphql(ctx, query, map[string]any{"owner": c.owner, "repo": c.name, "number": n, "after": after})
		if err != nil {
			return nil, err
		}
		threads := r.Data.Repository.PullRequest.ReviewThreads
		for _, node := range threads.Nodes {
			t := forge.Thread{ID: node.ID, File: node.Path, Line: node.Line, Resolved: node.IsResolved}
			for _, cm := range node.Comments.Nodes {
				t.Comments = append(t.Comments, comment(cm))
			}
			info := node.Comments.PageInfo
			for info.HasNextPage {
				next, err := c.graphql(ctx, `query($id:ID!,$after:String){node(id:$id){... on PullRequestReviewThread{comments(first:100,after:$after){nodes{id body url author{login} authorAssociation}pageInfo{hasNextPage endCursor}}}}}`, map[string]any{"id": node.ID, "after": info.EndCursor})
				if err != nil {
					return nil, err
				}
				for _, cm := range next.Data.Node.Comments.Nodes {
					t.Comments = append(t.Comments, comment(cm))
				}
				pi := next.Data.Node.Comments.PageInfo
				if pi.HasNextPage && pi.EndCursor == info.EndCursor {
					return nil, fmt.Errorf("GitHub repeated comment cursor")
				}
				info = pi
			}
			out = append(out, t)
		}
		if !threads.PageInfo.HasNextPage {
			break
		}
		if threads.PageInfo.EndCursor == after {
			return nil, fmt.Errorf("GitHub repeated thread cursor")
		}
		after = threads.PageInfo.EndCursor
	}
	return out, nil
}
func (c *Client) Reply(ctx context.Context, _ int, thread, text string) error {
	_, err := c.graphql(ctx, `mutation($thread:ID!,$body:String!){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$thread,body:$body}){comment{id}}}`, map[string]any{"thread": thread, "body": text})
	return err
}
