package forge

import (
	"regexp"
	"strconv"
	"time"
)

// Bounds of PRDetails (docs/plans/active/pr-panel.md, decision 8): third-party
// text is cut to these sizes and the matching Truncated flag is set.
const (
	DetailsBodyMax     = 32 << 10
	DetailsCommentMax  = 8 << 10
	DetailsMaxChecks   = 200
	DetailsMaxReviews  = 100
	DetailsMaxThreads  = 100
	DetailsMaxComments = 50
	CheckLogMaxLines   = 200
	CheckLogMaxBytes   = 16 << 10
)

// Check item states.
const (
	CheckSuccess = "success"
	CheckFailure = "failure"
	CheckPending = "pending"
	CheckSkipped = "skipped"
)

// Review item states.
const (
	ReviewItemApproved         = "approved"
	ReviewItemChangesRequested = "changes_requested"
	ReviewItemCommented        = "commented"
	ReviewItemDismissed        = "dismissed"
)

// PRDetails stale reasons (PR.Stale uses the same field with other values).
const (
	StaleBudget          = "budget"
	StaleBackoff         = "backoff"
	StaleRateLimited     = "rate_limited"
	StaleUnauthenticated = "unauthenticated"
	StaleUnavailable     = "unavailable"
	StaleNotFocused      = "not_focused"
)

// CheckItem is one status check of the PR's head commit. ID is "run:<runId>"
// or "run:<runId>:job:<jobId>" parsed from the check's details URL; empty when
// no Actions log can be named.
type CheckItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	DurationMs  *int64     `json:"durationMs,omitempty"`
	URL         string     `json:"url,omitempty"`
}

// DetailChecks is the PR panel's checks section.
type DetailChecks struct {
	Checks
	Items     []CheckItem `json:"items"`
	Truncated bool        `json:"truncated"`
}

// ReviewItem is one submitted review.
type ReviewItem struct {
	Author        string `json:"author"`
	State         string `json:"state"`
	SubmittedAt   string `json:"submittedAt"`
	Body          string `json:"body"`
	BodyTruncated bool   `json:"bodyTruncated"`
}

// DetailReviews is the PR panel's reviews section. Required is nil when the
// token cannot read the branch protection rule.
type DetailReviews struct {
	Approvals int          `json:"approvals"`
	Required  *int         `json:"required"`
	Items     []ReviewItem `json:"items"`
	Awaiting  []string     `json:"awaiting"`
	Truncated bool         `json:"truncated"`
}

// Comment is one comment of a review thread.
type Comment struct {
	Author        string `json:"author"`
	CreatedAt     string `json:"createdAt"`
	Body          string `json:"body"`
	BodyTruncated bool   `json:"bodyTruncated"`
}

// Thread is one review thread.
type Thread struct {
	ID                string    `json:"id"`
	IsResolved        bool      `json:"isResolved"`
	IsOutdated        bool      `json:"isOutdated"`
	Path              string    `json:"path"`
	Line              *int      `json:"line"`
	Comments          []Comment `json:"comments"`
	CommentsTruncated bool      `json:"commentsTruncated"`
}

// DetailThreads is the PR panel's threads section.
type DetailThreads struct {
	Items     []Thread `json:"items"`
	Truncated bool     `json:"truncated"`
}

// PRDetails is the PR panel's payload (task.prDetails). A provider fills
// everything but TaskID, Stale and RefreshAvailableAt, which the poller sets.
type PRDetails struct {
	TaskID             int64         `json:"taskId"`
	Number             int           `json:"number"`
	Repo               string        `json:"repo"`
	URL                string        `json:"url"`
	Title              string        `json:"title"`
	State              string        `json:"state"`
	Draft              bool          `json:"draft"`
	Mergeable          string        `json:"mergeable"`
	ReviewDecision     string        `json:"reviewDecision"`
	Body               string        `json:"body"`
	BodyTruncated      bool          `json:"bodyTruncated"`
	Checks             DetailChecks  `json:"checks"`
	Reviews            DetailReviews `json:"reviews"`
	Threads            DetailThreads `json:"threads"`
	FetchedAt          time.Time     `json:"fetchedAt"`
	Stale              string        `json:"stale"`
	RefreshAvailableAt string        `json:"refreshAvailableAt"`
}

// CheckLogTail is the tail of a failing check's log.
type CheckLogTail struct {
	CheckID   string    `json:"checkId"`
	Text      string    `json:"text"`
	Lines     int       `json:"lines"`
	Truncated bool      `json:"truncated"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// CodeCheckUnknown is the error for a check id the daemon did not issue in
// the task's cached details.
const CodeCheckUnknown = "check_unknown"

// CheckUnknown is the typed error for an unknown check id.
func CheckUnknown() *PRError {
	return NewPRError(CodeCheckUnknown, "").withMessage("That check isn't in this pull request's current details. Refresh and try again.")
}

var checkIDRe = regexp.MustCompile(`^run:([0-9]{1,18})(?::job:([0-9]{1,18}))?$`)

// ParseCheckID splits a daemon-issued check id ("run:<runId>[:job:<jobId>]").
// jobID is 0 when the id names a whole run.
func ParseCheckID(id string) (runID, jobID int64, ok bool) {
	m := checkIDRe.FindStringSubmatch(id)
	if m == nil {
		return 0, 0, false
	}
	runID, _ = strconv.ParseInt(m[1], 10, 64)
	if m[2] != "" {
		jobID, _ = strconv.ParseInt(m[2], 10, 64)
	}
	return runID, jobID, runID > 0
}
