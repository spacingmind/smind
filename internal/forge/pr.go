package forge

import "time"

// PR states (ADR-0039 section 3).
const (
	PRStateOpen   = "open"
	PRStateClosed = "closed"
	PRStateMerged = "merged"
)

// PR mergeable values.
const (
	MergeableYes         = "mergeable"
	MergeableConflicting = "conflicting"
	MergeableUnknown     = "unknown"
)

// PR review decisions.
const (
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
	ReviewRequired         = "review_required"
	ReviewNone             = "none"
)

// Checks rollup states.
const (
	ChecksSuccess = "success"
	ChecksFailure = "failure"
	ChecksPending = "pending"
	ChecksNone    = "none"
)

// Checks is the rolled-up CI state of a PR's head commit.
type Checks struct {
	State   string `json:"state"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Pending int    `json:"pending"`
	Total   int    `json:"total"`
}

// PR is the forge-neutral pull request snapshot (ADR-0039 section 3) and the
// wire shape of TaskGit.pr (gitstate.PRInfo is an alias). Stale and
// RefreshAvailableAt are filled by the poller when it serves a cached
// snapshot; a provider leaves them empty.
type PR struct {
	Number             int       `json:"number"`
	Repo               string    `json:"repo"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	State              string    `json:"state"`
	Draft              bool      `json:"draft"`
	HeadRef            string    `json:"headRef"`
	HeadSha            string    `json:"headSha"`
	BaseRef            string    `json:"baseRef"`
	FromFork           bool      `json:"fromFork"`
	Mergeable          string    `json:"mergeable"`
	ReviewDecision     string    `json:"reviewDecision"`
	Checks             *Checks   `json:"checks,omitempty"`
	FetchedAt          time.Time `json:"fetchedAt"`
	Stale              string    `json:"stale"`
	RefreshAvailableAt string    `json:"refreshAvailableAt"`
}

// Repo is the forge repository a directory's origin resolves to: the forge
// host ("github.com", or a GHE host) and "owner/name".
type Repo struct {
	Host          string
	NameWithOwner string
}
