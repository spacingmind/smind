package forge

import "context"

// Item is one PR/issue row in a Search result.
type Item struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updatedAt"`
	HeadRef   string `json:"headRef,omitempty"`
	// FromFork is true for a PR whose head lives in another repository
	// (isCrossRepository); only PR list rows carry it, so the picker can
	// badge a fork PR before a task is created from it (ADR-0039 section 7).
	FromFork bool `json:"fromFork,omitempty"`
}

// SearchResult is Search's answer: the repo the results belong to (the
// dir's repo, or the URL-named repo for a direct link) plus items in the
// forge's own order.
type SearchResult struct {
	Repo  string `json:"repo"`
	Items []Item `json:"items"`
}

// Snapshot is one PR/issue's full state, the payload an agent attaches
// for context (ADR-0038 §4). State is lowercase ("open"/"closed"/"merged").
type Snapshot struct {
	Number  int
	Repo    string
	Title   string
	URL     string
	State   string
	BaseRef string
	HeadRef string
	Body    string
}

// AuthStatus describes how a forge CLI is authenticated on the daemon host.
// Nothing fills it yet; it is the shape later PRs of ADR-0039 return.
type AuthStatus struct {
	CLIInstalled  bool
	Host, Login   string
	Authenticated bool
	Scopes        []string
	Protocol      string
	Source        string
}

// Provider is a forge client (GitHub today). Repo, OpenPRs and PR serve the
// PR poller (ADR-0039 section 4); CreatePR and Status arrive with their
// callers in later PRs. Failures are *Error.
type Provider interface {
	Name() string
	Search(ctx context.Context, dir, kind, query string, limit int) (SearchResult, error)
	Get(ctx context.Context, dir, kind string, number int, repo string) (*Snapshot, error)
	// Repo resolves dir's origin to its forge host and owner/name.
	Repo(ctx context.Context, dir string) (Repo, error)
	// OpenPRs lists the open PRs of dir's repo (one call, no cache).
	OpenPRs(ctx context.Context, dir string) ([]PR, error)
	// PR fetches one PR of dir's repo by number.
	PR(ctx context.Context, dir string, number int) (PR, error)
}
