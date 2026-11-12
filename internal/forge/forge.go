package forge

import "context"

type MR struct {
	Number                                           int
	URL, State, Title, Body, Head, Base, MergeStatus string
	Mergeable, Behind                                bool
}
type OpenRequest struct {
	Title, Body, Branch, Base string
	Labels                    []string
}
type Access struct {
	Read, Push    bool
	DefaultBranch string
}
type Job struct{ ID, Name, URL, State, Summary, LogID string }
type Checks struct {
	Jobs     []Job
	Required []string
}
type Comment struct {
	ID, Text, Author, URL string
	Writer                bool
}
type Thread struct {
	ID, File string
	Line     int
	Resolved bool
	Comments []Comment
}

// Client never pushes git refs; git retains its own credentials and lease rules.
type Client interface {
	Access(context.Context) (Access, error)
	FindMR(context.Context, string) (*MR, error)
	OpenMR(context.Context, OpenRequest) (*MR, error)
	GetMR(context.Context, int) (*MR, error)
	Checks(context.Context, string, string) (Checks, error)
	Log(context.Context, Job) (Log, error)
	Threads(context.Context, int) ([]Thread, error)
	Reply(context.Context, int, string, string) error
	Merge(context.Context, int, string, string) error
}
