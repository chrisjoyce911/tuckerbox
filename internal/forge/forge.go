package forge

import "context"

type MR struct {
	Number                                           int
	URL, State, Title, Body, Head, Base, MergeStatus string
	Author                                           string
	Draft                                            bool
	Mergeable, Behind                                bool
}
type OpenRequest struct {
	Title, Body, Branch, Base string
	Draft                     bool
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

// TeamClient adds authenticated ownership and protected-base operations.
// Keeping this separate lets solo shipping use a smaller forge surface.
type TeamClient interface {
	Client
	Login(context.Context) (string, error)
	ProtectedBase(context.Context, string) (bool, error)
	ListOpenMRs(context.Context) ([]MR, error)
	UpdateMR(context.Context, int, OpenRequest) (*MR, error)
	MarkReady(context.Context, int) error
}
