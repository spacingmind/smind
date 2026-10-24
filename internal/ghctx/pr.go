package ghctx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// prTimeout bounds one OpenPRs / PR call.
const prTimeout = 20 * time.Second

// knownRepoTTL is how long a Repo answer stays usable by OpenPRs/PR for
// their URL host check. It outlives the 60s repo cache and the poller's own
// 1 h Repo cache, so OpenPRs never has to spend an extra, unmetered
// `gh repo view` of its own.
const knownRepoTTL = 2 * time.Hour

// prFields is the --json field list shared by `gh pr list` and `gh pr view`.
const prFields = "number,url,title,state,isDraft,headRefName,headRefOid,baseRefName,isCrossRepository,mergeable,reviewDecision,statusCheckRollup"

// ghPRJSON is gh's --json shape for a PR. Every field is optional: absent or
// null values stay zero and map to the "unknown" end of each enum.
type ghPRJSON struct {
	Number            int           `json:"number"`
	URL               string        `json:"url"`
	Title             string        `json:"title"`
	State             string        `json:"state"`
	IsDraft           bool          `json:"isDraft"`
	HeadRefName       string        `json:"headRefName"`
	HeadRefOid        string        `json:"headRefOid"`
	BaseRefName       string        `json:"baseRefName"`
	IsCrossRepository bool          `json:"isCrossRepository"`
	Mergeable         string        `json:"mergeable"`
	ReviewDecision    string        `json:"reviewDecision"`
	StatusCheckRollup []ghCheckJSON `json:"statusCheckRollup"`
}

// ghCheckJSON is one statusCheckRollup item: a CheckRun (status/conclusion)
// or a StatusContext (state).
type ghCheckJSON struct {
	Typename   string `json:"__typename"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// Repo resolves dir's origin to its forge host and owner/name (cached 60s)
// and remembers it so OpenPRs/PR can validate PR URLs against the host
// without another gh call. A repo whose host gh does not report is an error:
// no PR URL could be validated against it.
func (c *Client) Repo(ctx context.Context, dir string) (forge.Repo, error) {
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	r, err := c.repoInfo(ctx, dir)
	if err != nil {
		return forge.Repo{}, err
	}
	if r.Host == "" {
		return forge.Repo{}, forge.NewError(forge.CodeError, "gh repo view returned no usable url")
	}
	c.known.set(dir, r)
	return r, nil
}

// knownRepo is the repo OpenPRs/PR validate against: the last Repo answer
// for dir, else a (cached) gh repo view.
func (c *Client) knownRepo(ctx context.Context, dir string) (forge.Repo, error) {
	if r, ok := c.known.get(dir); ok {
		return r.(forge.Repo), nil
	}
	return c.Repo(ctx, dir)
}

// OpenPRs lists dir's open PRs with one `gh pr list` call. Concurrent calls
// for the same dir share it (single-flight); the result is never cached, the
// poller owns the cadence. A PR whose URL is not a pull URL on the repo's
// host is dropped.
func (c *Client) OpenPRs(ctx context.Context, dir string) ([]forge.PR, error) {
	v, err := c.flights.do("o\x00"+dir, func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, prTimeout)
		defer cancel()
		repo, err := c.knownRepo(ctx, dir)
		if err != nil {
			return nil, err
		}
		out, err := c.run(ctx, dir, "pr", "list", "--state", "open", "--limit", "100", "--json", prFields)
		if err != nil {
			return nil, c.mapRunError(ctx, err)
		}
		var rows []ghPRJSON
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, forge.NewError(forge.CodeError, "parse gh pr list output: "+err.Error())
		}
		now := c.ttlNow()
		prs := make([]forge.PR, 0, len(rows))
		for _, r := range rows {
			if pr, ok := mapPR(r, repo, now); ok {
				prs = append(prs, pr)
			}
		}
		return prs, nil
	})
	if err != nil {
		return nil, err
	}
	return clonePRs(v.([]forge.PR)), nil
}

// prByHeadFields is the lighter field list of PRsByHead: no check rollup, the
// slowest field and one the archive lookup never reads.
const prByHeadFields = "number,url,title,state,isDraft,headRefName,headRefOid,baseRefName,isCrossRepository,mergeable,reviewDecision"

// prByHeadLimit caps PRsByHead: a branch name is rarely reused more than a
// few times.
const prByHeadLimit = 5

// PRsByHead lists dir's PRs (any state) whose head branch is headRef with one
// `gh pr list --head` call. Concurrent calls for the same dir and branch share
// it; the result is never cached.
func (c *Client) PRsByHead(ctx context.Context, dir, headRef string) ([]forge.PR, error) {
	if headRef == "" || strings.HasPrefix(headRef, "-") || strings.ContainsAny(headRef, "\x00\n") {
		return nil, fmt.Errorf("invalid arguments")
	}
	v, err := c.flights.do("h\x00"+dir+"\x00"+headRef, func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, prTimeout)
		defer cancel()
		repo, err := c.knownRepo(ctx, dir)
		if err != nil {
			return nil, err
		}
		out, err := c.run(ctx, dir, "pr", "list", "--head", headRef, "--state", "all",
			"--limit", fmt.Sprint(prByHeadLimit), "--json", prByHeadFields)
		if err != nil {
			return nil, c.mapRunError(ctx, err)
		}
		var rows []ghPRJSON
		if err := json.Unmarshal([]byte(out), &rows); err != nil {
			return nil, forge.NewError(forge.CodeError, "parse gh pr list output: "+err.Error())
		}
		now := c.ttlNow()
		prs := make([]forge.PR, 0, len(rows))
		for _, r := range rows {
			if pr, ok := mapPR(r, repo, now); ok {
				prs = append(prs, pr)
			}
		}
		return prs, nil
	})
	if err != nil {
		return nil, err
	}
	return clonePRs(v.([]forge.PR)), nil
}

// PR fetches one PR of dir's repo with `gh pr view <number>`.
func (c *Client) PR(ctx context.Context, dir string, number int) (forge.PR, error) {
	if number <= 0 {
		return forge.PR{}, fmt.Errorf("invalid arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	repo, err := c.knownRepo(ctx, dir)
	if err != nil {
		return forge.PR{}, err
	}
	out, err := c.run(ctx, dir, "pr", "view", fmt.Sprint(number), "--json", prFields)
	if err != nil {
		return forge.PR{}, c.mapRunError(ctx, err)
	}
	var r ghPRJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return forge.PR{}, forge.NewError(forge.CodeError, "parse gh pr view output: "+err.Error())
	}
	pr, ok := mapPR(r, repo, c.ttlNow())
	if !ok {
		return forge.PR{}, forge.NewError(forge.CodeError, "gh pr view returned an unusable pull request url")
	}
	return pr, nil
}

// clonePRs copies prs deeply enough that single-flight waiters never share a
// *Checks.
func clonePRs(prs []forge.PR) []forge.PR {
	out := make([]forge.PR, len(prs))
	for i, p := range prs {
		if p.Checks != nil {
			c := *p.Checks
			p.Checks = &c
		}
		out[i] = p
	}
	return out
}

// mapPR converts gh's JSON to a forge.PR; false means the PR is unusable
// (number missing, or its url is not a pull url on repo's host).
func mapPR(r ghPRJSON, repo forge.Repo, now time.Time) (forge.PR, bool) {
	u, ok := forge.ValidPRURL(r.URL, repo.Host)
	if !ok || r.Number <= 0 {
		return forge.PR{}, false
	}
	return forge.PR{
		Number:         r.Number,
		Repo:           repo.NameWithOwner,
		URL:            u,
		Title:          r.Title,
		State:          mapPRState(r.State),
		Draft:          r.IsDraft,
		HeadRef:        r.HeadRefName,
		HeadSha:        r.HeadRefOid,
		BaseRef:        r.BaseRefName,
		FromFork:       r.IsCrossRepository,
		Mergeable:      mapMergeable(r.Mergeable),
		ReviewDecision: mapReviewDecision(r.ReviewDecision),
		Checks:         rollupChecks(r.StatusCheckRollup),
		FetchedAt:      now,
	}, true
}

// mapPRState maps gh's OPEN/CLOSED/MERGED; `gh pr list --state open` only
// returns open PRs, so anything unrecognised is treated as open.
func mapPRState(s string) string {
	switch strings.ToUpper(s) {
	case "CLOSED":
		return forge.PRStateClosed
	case "MERGED":
		return forge.PRStateMerged
	default:
		return forge.PRStateOpen
	}
}

func mapMergeable(s string) string {
	switch strings.ToUpper(s) {
	case "MERGEABLE":
		return forge.MergeableYes
	case "CONFLICTING":
		return forge.MergeableConflicting
	default:
		return forge.MergeableUnknown
	}
}

func mapReviewDecision(s string) string {
	switch strings.ToUpper(s) {
	case "APPROVED":
		return forge.ReviewApproved
	case "CHANGES_REQUESTED":
		return forge.ReviewChangesRequested
	case "REVIEW_REQUIRED":
		return forge.ReviewRequired
	default:
		return forge.ReviewNone
	}
}

// rollupChecks folds statusCheckRollup into counts and one state. Items of
// an unrecognised type are skipped; an unrecognised conclusion/state on a
// recognised item counts as pending (it is neither a pass nor a failure we
// can name). No countable items gives state "none".
func rollupChecks(items []ghCheckJSON) *forge.Checks {
	var c forge.Checks
	for _, it := range items {
		switch checkOutcome(it) {
		case "passed":
			c.Passed++
		case "failed":
			c.Failed++
		case "pending":
			c.Pending++
		}
	}
	c.Total = c.Passed + c.Failed + c.Pending
	switch {
	case c.Total == 0:
		c.State = forge.ChecksNone
	case c.Failed > 0:
		c.State = forge.ChecksFailure
	case c.Pending > 0:
		c.State = forge.ChecksPending
	default:
		c.State = forge.ChecksSuccess
	}
	return &c
}

// checkOutcome classifies one rollup item as passed, failed, pending or ""
// (not a check).
func checkOutcome(it ghCheckJSON) string {
	isRun := strings.EqualFold(it.Typename, "CheckRun") || (it.Typename == "" && it.Status != "")
	isContext := strings.EqualFold(it.Typename, "StatusContext") || (it.Typename == "" && it.Status == "" && it.State != "")
	switch {
	case isRun:
		if !strings.EqualFold(it.Status, "COMPLETED") {
			return "pending"
		}
		switch strings.ToUpper(it.Conclusion) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			return "passed"
		case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "failed"
		default:
			return "pending"
		}
	case isContext:
		switch strings.ToUpper(it.State) {
		case "SUCCESS":
			return "passed"
		case "FAILURE", "ERROR":
			return "failed"
		default:
			return "pending"
		}
	}
	return ""
}
