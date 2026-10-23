package ghctx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// detailsFields is the --json field list of the PR panel's `gh pr view`.
const detailsFields = "title,url,state,isDraft,mergeable,reviewDecision,reviews,reviewRequests,statusCheckRollup,body,number"

// threadsQuery is the fixed GraphQL document of the PR panel. Owner, name and
// number travel as variables (-f / -F), never spliced into this text.
const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){` +
	`reviewThreads(first:100){totalCount nodes{id isResolved isOutdated path line comments(first:50){totalCount nodes{author{login} createdAt bodyText}}}}` +
	`baseRef{branchProtectionRule{requiredApprovingReviewCount}}}}}`

type ghLogin struct {
	Login string `json:"login"`
}

type ghDetailCheck struct {
	Typename    string `json:"__typename"`
	Name        string `json:"name"`
	Context     string `json:"context"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	State       string `json:"state"`
	StartedAt   string `json:"startedAt"`
	CompletedAt string `json:"completedAt"`
	DetailsURL  string `json:"detailsUrl"`
	TargetURL   string `json:"targetUrl"`
}

type ghDetailsJSON struct {
	ghPRJSON
	Body    string `json:"body"`
	Reviews []struct {
		Author      *ghLogin `json:"author"`
		State       string   `json:"state"`
		SubmittedAt string   `json:"submittedAt"`
		Body        string   `json:"body"`
	} `json:"reviews"`
	ReviewRequests []struct {
		Typename string `json:"__typename"`
		Login    string `json:"login"`
		Name     string `json:"name"`
		Slug     string `json:"slug"`
	} `json:"reviewRequests"`
	Rollup []ghDetailCheck `json:"statusCheckRollup"`
}

type ghThreadsJSON struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					TotalCount int `json:"totalCount"`
					Nodes      []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						IsOutdated bool   `json:"isOutdated"`
						Path       string `json:"path"`
						Line       *int   `json:"line"`
						Comments   struct {
							TotalCount int `json:"totalCount"`
							Nodes      []struct {
								Author    *ghLogin `json:"author"`
								CreatedAt string   `json:"createdAt"`
								BodyText  string   `json:"bodyText"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
				BaseRef *struct {
					Rule *struct {
						Required *int `json:"requiredApprovingReviewCount"`
					} `json:"branchProtectionRule"`
				} `json:"baseRef"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

var (
	runPathRe  = regexp.MustCompile(`/actions/runs/([0-9]{1,18})(?:/job/([0-9]{1,18}))?/?$`)
	checkIDRe  = regexp.MustCompile(`^run:([0-9]{1,18})(?::job:([0-9]{1,18}))?$`)
	ownerRepoR = regexp.MustCompile(`^([A-Za-z0-9_.-]{1,100})/([A-Za-z0-9_.-]{1,100})$`)
)

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

// PRDetails fetches the PR panel's payload: `gh pr view --json ...` and one
// `gh api graphql` for threads and the required-approval count. Each call has
// its own prTimeout deadline. Nothing is cached here; the poller owns cadence.
func (c *Client) PRDetails(ctx context.Context, dir string, number int) (forge.PRDetails, error) {
	if number <= 0 {
		return forge.PRDetails{}, fmt.Errorf("invalid arguments")
	}
	repo, err := func() (forge.Repo, error) {
		ctx, cancel := context.WithTimeout(ctx, prTimeout)
		defer cancel()
		return c.knownRepo(ctx, dir)
	}()
	if err != nil {
		return forge.PRDetails{}, err
	}
	m := ownerRepoR.FindStringSubmatch(repo.NameWithOwner)
	if m == nil || !forge.ValidHost(repo.Host) {
		return forge.PRDetails{}, forge.NewError(forge.CodeError, "unusable repository name or host")
	}

	viewCtx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	out, err := c.run(viewCtx, dir, "pr", "view", strconv.Itoa(number), "--json", detailsFields)
	if err != nil {
		return forge.PRDetails{}, c.mapRunError(viewCtx, err)
	}
	var v ghDetailsJSON
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return forge.PRDetails{}, forge.NewError(forge.CodeError, "parse gh pr view output: "+err.Error())
	}
	prURL, ok := forge.ValidPRURL(v.URL, repo.Host)
	if !ok || v.Number != number {
		return forge.PRDetails{}, forge.NewError(forge.CodeError, "gh pr view returned an unusable pull request url")
	}

	gqlCtx, cancel2 := context.WithTimeout(ctx, prTimeout)
	defer cancel2()
	args := []string{"api", "graphql"}
	if !strings.EqualFold(repo.Host, "github.com") {
		args = append(args, "--hostname", repo.Host)
	}
	args = append(args, "-f", "query="+threadsQuery, "-f", "owner="+m[1], "-f", "name="+m[2], "-F", "number="+strconv.Itoa(number))
	gout, err := c.run(gqlCtx, dir, args...)
	if err != nil {
		return forge.PRDetails{}, c.mapRunError(gqlCtx, err)
	}
	var g ghThreadsJSON
	if err := json.Unmarshal([]byte(gout), &g); err != nil {
		return forge.PRDetails{}, forge.NewError(forge.CodeError, "parse gh api graphql output: "+err.Error())
	}

	d := forge.PRDetails{
		Number:         number,
		Repo:           repo.NameWithOwner,
		URL:            prURL,
		Title:          forge.SanitizeText(v.Title),
		State:          mapPRState(v.State),
		Draft:          v.IsDraft,
		Mergeable:      mapMergeable(v.Mergeable),
		ReviewDecision: mapReviewDecision(v.ReviewDecision),
		FetchedAt:      c.ttlNow(),
	}
	d.Body, d.BodyTruncated = forge.BoundText(v.Body, forge.DetailsBodyMax)
	d.Checks = mapDetailChecks(v.Rollup, repo.Host)
	mapDetailReviews(&d.Reviews, v)
	if br := g.Data.Repository.PullRequest.BaseRef; br != nil && br.Rule != nil {
		d.Reviews.Required = br.Rule.Required
	}
	d.Threads = mapThreads(g)
	return d, nil
}

func clipField(s string, n int) string {
	s, _ = forge.BoundText(s, n)
	return s
}

func login(l *ghLogin) string {
	if l == nil || l.Login == "" {
		return "ghost"
	}
	return clipField(l.Login, 100)
}

// checkURL keeps a check's link only when it is http(s) on the forge host;
// it is rendered as a clickable link, so anything else is dropped.
func checkURL(raw, host string) (string, *url.URL) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Opaque != "" ||
		!strings.EqualFold(u.Host, host) {
		return "", nil
	}
	return u.String(), u
}

func mapDetailChecks(items []ghDetailCheck, host string) forge.DetailChecks {
	out := forge.DetailChecks{Items: []forge.CheckItem{}}
	var counts forge.Checks
	for _, it := range items {
		plain := ghCheckJSON{Typename: it.Typename, Status: it.Status, Conclusion: it.Conclusion, State: it.State}
		outcome := checkOutcome(plain)
		if outcome == "" {
			continue
		}
		state := forge.CheckPending
		switch outcome {
		case "passed":
			counts.Passed++
			state = forge.CheckSuccess
			if strings.EqualFold(it.Conclusion, "SKIPPED") {
				state = forge.CheckSkipped
			}
		case "failed":
			counts.Failed++
			state = forge.CheckFailure
		default:
			counts.Pending++
		}
		if len(out.Items) >= forge.DetailsMaxChecks {
			out.Truncated = true
			continue
		}
		ci := forge.CheckItem{State: state, Status: clipField(it.Status, 40), Conclusion: clipField(it.Conclusion, 40)}
		name := it.Name
		raw := it.DetailsURL
		if strings.EqualFold(it.Typename, "StatusContext") || (it.Typename == "" && it.Status == "") {
			name, raw = it.Context, it.TargetURL
			ci.Conclusion = clipField(it.State, 40)
		}
		ci.Name = clipField(name, 512)
		if link, u := checkURL(raw, host); u != nil {
			ci.URL = link
			if m := runPathRe.FindStringSubmatch(u.Path); m != nil {
				ci.ID = "run:" + m[1]
				if m[2] != "" {
					ci.ID += ":job:" + m[2]
				}
			}
		}
		var st, en time.Time
		if t, err := time.Parse(time.RFC3339, it.StartedAt); err == nil && !t.IsZero() && t.Year() > 1 {
			st = t
			ci.StartedAt = &st
		}
		if t, err := time.Parse(time.RFC3339, it.CompletedAt); err == nil && t.Year() > 1 {
			en = t
			ci.CompletedAt = &en
		}
		if ci.StartedAt != nil && ci.CompletedAt != nil && !en.Before(st) {
			ms := en.Sub(st).Milliseconds()
			ci.DurationMs = &ms
		}
		out.Items = append(out.Items, ci)
	}
	counts.Total = counts.Passed + counts.Failed + counts.Pending
	switch {
	case counts.Total == 0:
		counts.State = forge.ChecksNone
	case counts.Failed > 0:
		counts.State = forge.ChecksFailure
	case counts.Pending > 0:
		counts.State = forge.ChecksPending
	default:
		counts.State = forge.ChecksSuccess
	}
	out.Checks = counts
	return out
}

func mapDetailReviews(out *forge.DetailReviews, v ghDetailsJSON) {
	out.Items = []forge.ReviewItem{}
	out.Awaiting = []string{}
	latest := map[string]string{} // reviewer -> last decisive state
	for _, r := range v.Reviews {
		state := ""
		switch strings.ToUpper(r.State) {
		case "APPROVED":
			state = forge.ReviewItemApproved
		case "CHANGES_REQUESTED":
			state = forge.ReviewItemChangesRequested
		case "COMMENTED":
			state = forge.ReviewItemCommented
		case "DISMISSED":
			state = forge.ReviewItemDismissed
		default: // PENDING is the viewer's own draft
			continue
		}
		who := login(r.Author)
		if state != forge.ReviewItemCommented {
			latest[who] = state
		}
		item := forge.ReviewItem{Author: who, State: state, SubmittedAt: clipField(r.SubmittedAt, 40)}
		item.Body, item.BodyTruncated = forge.BoundText(r.Body, forge.DetailsCommentMax)
		out.Items = append(out.Items, item)
	}
	for _, s := range latest {
		if s == forge.ReviewItemApproved {
			out.Approvals++
		}
	}
	if n := len(out.Items); n > forge.DetailsMaxReviews {
		out.Items = out.Items[n-forge.DetailsMaxReviews:]
		out.Truncated = true
	}
	for _, rq := range v.ReviewRequests {
		who := rq.Login
		if who == "" {
			who = rq.Slug
		}
		if who == "" {
			who = rq.Name
		}
		if who = clipField(who, 100); who != "" && len(out.Awaiting) < forge.DetailsMaxReviews {
			out.Awaiting = append(out.Awaiting, who)
		}
	}
}

func mapThreads(g ghThreadsJSON) forge.DetailThreads {
	rt := g.Data.Repository.PullRequest.ReviewThreads
	out := forge.DetailThreads{Items: []forge.Thread{}}
	for _, n := range rt.Nodes {
		if len(out.Items) >= forge.DetailsMaxThreads {
			out.Truncated = true
			break
		}
		t := forge.Thread{
			ID: clipField(n.ID, 128), IsResolved: n.IsResolved, IsOutdated: n.IsOutdated,
			Path: clipField(n.Path, 1024), Line: n.Line, Comments: []forge.Comment{},
		}
		for _, cm := range n.Comments.Nodes {
			if len(t.Comments) >= forge.DetailsMaxComments {
				t.CommentsTruncated = true
				break
			}
			c := forge.Comment{Author: login(cm.Author), CreatedAt: clipField(cm.CreatedAt, 40)}
			c.Body, c.BodyTruncated = forge.BoundText(cm.BodyText, forge.DetailsCommentMax)
			t.Comments = append(t.Comments, c)
		}
		if n.Comments.TotalCount > len(t.Comments) {
			t.CommentsTruncated = true
		}
		out.Items = append(out.Items, t)
	}
	if rt.TotalCount > len(out.Items) {
		out.Truncated = true
	}
	return out
}

// CheckLog runs `gh run view <run> [--job <job>] --log-failed` and returns
// the sanitised tail (last 200 lines / 16 KiB). When earlier output was cut
// the text starts with forge.LogCutMarker. Only integers reach argv.
func (c *Client) CheckLog(ctx context.Context, dir string, runID, jobID int64) (string, error) {
	if runID <= 0 || jobID < 0 {
		return "", fmt.Errorf("invalid arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, prTimeout)
	defer cancel()
	args := []string{"run", "view", strconv.FormatInt(runID, 10)}
	if jobID > 0 {
		args = append(args, "--job", strconv.FormatInt(jobID, 10))
	}
	args = append(args, "--log-failed")
	out, err := c.run(ctx, dir, args...)
	if err != nil {
		return "", c.mapRunError(ctx, err)
	}
	tail, cut := forge.TailLog(out)
	if cut {
		tail = forge.LogCutMarker + "\n" + tail
	}
	return tail, nil
}
