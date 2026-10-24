package forge

import (
	"context"
	"strings"
)

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

// AuthHost is one forge host's sign-in state on the daemon host. Source is
// where gh keeps the credential ("keyring", "file", "env" or "none"); it is
// never a path, and no field ever carries a token.
type AuthHost struct {
	Host          string   `json:"host"`
	Authenticated bool     `json:"authenticated"`
	Login         string   `json:"login"`
	Scopes        []string `json:"scopes"`
	Protocol      string   `json:"protocol"`
	Source        string   `json:"source"`
}

// CLIInfo says whether the forge CLI is on the daemon host's PATH.
type CLIInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

// AuthStatus is how a forge CLI is authenticated on the daemon host: the CLI
// and every host it knows (empty when the CLI is missing or signed out
// everywhere).
type AuthStatus struct {
	CLI   CLIInfo    `json:"cli"`
	Hosts []AuthHost `json:"hosts"`
}

// Authenticated reports whether host is signed in.
func (s AuthStatus) Authenticated(host string) bool {
	for _, h := range s.Hosts {
		if strings.EqualFold(h.Host, host) && h.Authenticated {
			return true
		}
	}
	return false
}

// CreatePRInput is what Provider.CreatePR opens a pull request from. Head is
// a branch already pushed to the repo's origin; Body is passed to the forge
// CLI on stdin, never in argv.
type CreatePRInput struct {
	Base  string
	Head  string
	Title string
	Body  string
	Draft bool
}

// Provider is a forge client (GitHub today). Repo, OpenPRs and PR serve the
// PR poller (ADR-0039 section 4); CreatePR serves task.createPr (section 5);
// Status, Login and Logout serve forge.status, forge.setToken and
// forge.logout (section 8). Failures are *Error.
type Provider interface {
	Name() string
	Search(ctx context.Context, dir, kind, query string, limit int) (SearchResult, error)
	Get(ctx context.Context, dir, kind string, number int, repo string) (*Snapshot, error)
	// Repo resolves dir's origin to its forge host and owner/name.
	Repo(ctx context.Context, dir string) (Repo, error)
	// OpenPRs lists the open PRs of dir's repo (one call, no cache).
	OpenPRs(ctx context.Context, dir string) ([]PR, error)
	// PRsByHead lists up to a handful of PRs of dir's repo, in any state,
	// whose head branch is headRef: one call, for an explicit user action
	// only (the archive preview's lookup for a task with no stored PR).
	PRsByHead(ctx context.Context, dir, headRef string) ([]PR, error)
	// PR fetches one PR of dir's repo by number.
	PR(ctx context.Context, dir string, number int) (PR, error)
	// PRDetails fetches the PR panel's payload for one PR: two gh calls
	// (the view and the GraphQL threads). Text is bounded and sanitised.
	PRDetails(ctx context.Context, dir string, number int) (PRDetails, error)
	// CheckLog returns the sanitised tail (<= 200 lines / 16 KiB) of a failed
	// check's log: one call. runID and jobID come from a daemon-issued check id.
	CheckLog(ctx context.Context, dir string, runID, jobID int64) (string, error)
	// CreatePR opens a pull request and returns its full state.
	CreatePR(ctx context.Context, dir string, in CreatePRInput) (PR, error)
	// Status reads the CLI's sign-in state for every host it knows. A missing
	// CLI is a status (CLI.Installed false), not an error.
	Status(ctx context.Context) (AuthStatus, error)
	// Login signs the CLI in to host with token. The token travels on the
	// CLI's stdin only: never argv, env, a log line or an error.
	Login(ctx context.Context, host, token string) error
	// Logout signs the CLI out of host.
	Logout(ctx context.Context, host string) error
}
