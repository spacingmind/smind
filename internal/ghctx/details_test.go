package ghctx

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

const detailsViewJSON = `{
 "number": 5, "title": "Add thing", "url": "https://github.com/spacingmind/smind/pull/5",
 "state": "OPEN", "isDraft": true, "mergeable": "CONFLICTING", "reviewDecision": "REVIEW_REQUIRED",
 "body": "hello",
 "reviews": [
  {"author": {"login": "alice"}, "state": "CHANGES_REQUESTED", "submittedAt": "2026-10-20T10:00:00Z", "body": "fix it"},
  {"author": {"login": "alice"}, "state": "APPROVED", "submittedAt": "2026-10-21T10:00:00Z", "body": ""},
  {"author": {"login": "bob"}, "state": "COMMENTED", "submittedAt": "2026-10-21T11:00:00Z", "body": "nit"},
  {"author": null, "state": "PENDING", "submittedAt": "", "body": "draft"}
 ],
 "reviewRequests": [{"__typename": "User", "login": "carol"}, {"__typename": "Team", "name": "Core", "slug": "core"}],
 "statusCheckRollup": [
  {"__typename": "CheckRun", "name": "build", "status": "COMPLETED", "conclusion": "SUCCESS",
   "startedAt": "2026-10-23T10:00:00Z", "completedAt": "2026-10-23T10:01:30Z",
   "detailsUrl": "https://github.com/spacingmind/smind/actions/runs/111/job/222"},
  {"__typename": "CheckRun", "name": "lint", "status": "COMPLETED", "conclusion": "FAILURE",
   "detailsUrl": "https://github.com/spacingmind/smind/actions/runs/333"},
  {"__typename": "CheckRun", "name": "e2e", "status": "IN_PROGRESS", "conclusion": "",
   "detailsUrl": "https://evil.example/actions/runs/9"},
  {"__typename": "CheckRun", "name": "docs", "status": "COMPLETED", "conclusion": "SKIPPED", "detailsUrl": "javascript:alert(1)"},
  {"__typename": "StatusContext", "context": "ci/legacy", "state": "SUCCESS", "targetUrl": "https://github.com/spacingmind/smind/status/1"}
 ]
}`

const detailsGraphQLJSON = `{"data":{"repository":{"pullRequest":{
 "reviewThreads":{"totalCount":2,"nodes":[
  {"id":"T1","isResolved":false,"isOutdated":false,"path":"a/b.go","line":42,"comments":{"totalCount":2,"nodes":[
    {"author":{"login":"alice"},"createdAt":"2026-10-21T10:00:00Z","bodyText":"why?"},
    {"author":null,"createdAt":"2026-10-21T11:00:00Z","bodyText":"because"}]}},
  {"id":"T2","isResolved":true,"isOutdated":true,"path":"c.go","line":null,"comments":{"totalCount":1,"nodes":[
    {"author":{"login":"bob"},"createdAt":"2026-10-21T12:00:00Z","bodyText":"ok"}]}}]},
 "baseRef":null}}}}`

func detailsStub(t *testing.T, view, gql string) *stubRunner {
	return &stubRunner{fn: func(call stubCall) (string, error) {
		switch {
		case call.args[0] == "repo":
			return repoViewJSON, nil
		case call.args[0] == "pr" && call.args[1] == "view":
			return view, nil
		case call.args[0] == "api":
			return gql, nil
		}
		t.Fatalf("unexpected gh call %v", call.args)
		return "", nil
	}}
}

func TestPRDetails_MapsGraphQLThreads(t *testing.T) {
	s := detailsStub(t, detailsViewJSON, detailsGraphQLJSON)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	c := New(WithRunner(s.run), WithNow(func() time.Time { return now }))
	d, err := c.PRDetails(context.Background(), "/w", 5)
	if err != nil {
		t.Fatal(err)
	}
	if d.Number != 5 || d.Repo != "spacingmind/smind" || d.State != "open" || !d.Draft || d.Mergeable != "conflicting" ||
		d.ReviewDecision != "review_required" || d.Body != "hello" || !d.FetchedAt.Equal(now) {
		t.Fatalf("details = %+v", d)
	}
	if len(d.Threads.Items) != 2 || d.Threads.Truncated {
		t.Fatalf("threads = %+v", d.Threads)
	}
	t1, t2 := d.Threads.Items[0], d.Threads.Items[1]
	if t1.ID != "T1" || t1.Path != "a/b.go" || t1.Line == nil || *t1.Line != 42 || t1.IsResolved || len(t1.Comments) != 2 ||
		t1.Comments[1].Author != "ghost" || t1.Comments[0].Body != "why?" {
		t.Fatalf("thread 1 = %+v", t1)
	}
	if !t2.IsResolved || !t2.IsOutdated || t2.Line != nil {
		t.Fatalf("thread 2 = %+v", t2)
	}
	if d.Reviews.Required != nil || d.Reviews.Approvals != 1 || len(d.Reviews.Items) != 3 ||
		strings.Join(d.Reviews.Awaiting, ",") != "carol,core" {
		t.Fatalf("reviews = %+v", d.Reviews)
	}
	ch := d.Checks
	if ch.State != "failure" || ch.Passed != 3 || ch.Failed != 1 || ch.Pending != 1 || ch.Total != 5 || len(ch.Items) != 5 {
		t.Fatalf("checks = %+v", ch)
	}
	b := ch.Items[0]
	if b.ID != "run:111:job:222" || b.State != "success" || b.DurationMs == nil || *b.DurationMs != 90000 ||
		b.URL != "https://github.com/spacingmind/smind/actions/runs/111/job/222" {
		t.Fatalf("build check = %+v", b)
	}
	if ch.Items[1].ID != "run:333" || ch.Items[1].State != "failure" {
		t.Fatalf("lint check = %+v", ch.Items[1])
	}
	if e := ch.Items[2]; e.State != "pending" || e.ID != "" || e.URL != "" {
		t.Fatalf("off-host check kept id/url: %+v", e)
	}
	if e := ch.Items[3]; e.State != "skipped" || e.URL != "" {
		t.Fatalf("skipped check = %+v", e)
	}
	if e := ch.Items[4]; e.Name != "ci/legacy" || e.State != "success" || e.ID != "" || e.URL == "" {
		t.Fatalf("status context = %+v", e)
	}

	// Required approvals present.
	gql := strings.Replace(detailsGraphQLJSON, `"baseRef":null`, `"baseRef":{"branchProtectionRule":{"requiredApprovingReviewCount":2}}`, 1)
	c = New(WithRunner(detailsStub(t, detailsViewJSON, gql).run))
	d, err = c.PRDetails(context.Background(), "/w", 5)
	if err != nil || d.Reviews.Required == nil || *d.Reviews.Required != 2 {
		t.Fatalf("required = %+v, err %v", d.Reviews.Required, err)
	}

	// Argv: two calls, variables not interpolated into the query.
	pr, api := s.callsWith("pr"), s.callsWith("api")
	if len(pr) != 1 || len(api) != 1 {
		t.Fatalf("calls pr=%d api=%d", len(pr), len(api))
	}
	if strings.Join(pr[0].args, " ") != "pr view 5 --json "+detailsFields {
		t.Fatalf("view argv = %v", pr[0].args)
	}
	joined := strings.Join(api[0].args, "\x00")
	for _, want := range []string{"owner=spacingmind", "name=smind", "number=5", "query=" + threadsQuery} {
		if !strings.Contains(joined, want) {
			t.Fatalf("graphql argv %v lacks %q", api[0].args, want)
		}
	}
	if strings.Contains(threadsQuery, "spacingmind") {
		t.Fatal("query text embeds the owner")
	}
}

func TestPRDetails_BoundsAndSanitises(t *testing.T) {
	var reviews, checks, threads []string
	for i := 0; i < 120; i++ {
		reviews = append(reviews, fmt.Sprintf(`{"author":{"login":"u%d"},"state":"COMMENTED","body":"x"}`, i))
	}
	for i := 0; i < 230; i++ {
		checks = append(checks, fmt.Sprintf(`{"__typename":"CheckRun","name":"c%d","status":"COMPLETED","conclusion":"SUCCESS"}`, i))
	}
	long := strings.Repeat("é", 20000) // 40000 bytes
	var cms []string
	for i := 0; i < 60; i++ {
		cms = append(cms, `{"author":{"login":"a"},"createdAt":"t","bodyText":"`+long+`\u0000bad\u0007\u001b[31m\tok\nline"}`)
	}
	for i := 0; i < 110; i++ {
		threads = append(threads, fmt.Sprintf(`{"id":"T%d","path":"p","line":1,"comments":{"totalCount":60,"nodes":[%s]}}`, i, strings.Join(cms, ",")))
	}
	view := `{"number":5,"title":"t\u0000x","url":"https://github.com/spacingmind/smind/pull/5","state":"OPEN",
	 "body":"` + strings.Repeat("b", 40000) + `\u0000z","reviews":[` + strings.Join(reviews, ",") + `],
	 "statusCheckRollup":[` + strings.Join(checks, ",") + `]}`
	gql := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"totalCount":150,"nodes":[` + strings.Join(threads, ",") + `]}}}}}`
	c := New(WithRunner(detailsStub(t, view, gql).run))
	d, err := c.PRDetails(context.Background(), "/w", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Body) != forge.DetailsBodyMax || !d.BodyTruncated || strings.ContainsRune(d.Body, 0) || d.Title != "tx" {
		t.Fatalf("body %d truncated %v title %q", len(d.Body), d.BodyTruncated, d.Title)
	}
	if len(d.Reviews.Items) != 100 || !d.Reviews.Truncated {
		t.Fatalf("reviews %d truncated %v", len(d.Reviews.Items), d.Reviews.Truncated)
	}
	if len(d.Checks.Items) != 200 || !d.Checks.Truncated || d.Checks.Total != 230 {
		t.Fatalf("checks %d truncated %v total %d", len(d.Checks.Items), d.Checks.Truncated, d.Checks.Total)
	}
	if len(d.Threads.Items) != 100 || !d.Threads.Truncated {
		t.Fatalf("threads %d truncated %v", len(d.Threads.Items), d.Threads.Truncated)
	}
	th := d.Threads.Items[0]
	if len(th.Comments) != 50 || !th.CommentsTruncated {
		t.Fatalf("comments %d truncated %v", len(th.Comments), th.CommentsTruncated)
	}
	cm := th.Comments[0]
	if len(cm.Body) > forge.DetailsCommentMax || !cm.BodyTruncated {
		t.Fatalf("comment %d bytes truncated %v", len(cm.Body), cm.BodyTruncated)
	}
	// 4 KiB of é, cut on a rune boundary.
	if !strings.HasPrefix(cm.Body, "éé") || strings.ToValidUTF8(cm.Body, "") != cm.Body {
		t.Fatal("comment not valid UTF-8")
	}
	clean := forge.SanitizeText("a\x00b\x07\x1b[31m\tok\r\nline\x7f")
	if clean != "ab[31m\tok\nline\x7f" {
		t.Fatalf("sanitised = %q", clean)
	}
}

func TestPRDetails_ArgvAndEnvHaveNoToken(t *testing.T) {
	const hostile = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	t.Setenv("GH_TOKEN", hostile)
	t.Setenv("GITHUB_TOKEN", hostile)
	s := detailsStub(t, detailsViewJSON, detailsGraphQLJSON)
	c := New(WithRunner(s.run))
	if _, err := c.PRDetails(context.Background(), "/w", 5); err != nil {
		t.Fatal(err)
	}
	for _, call := range s.calls {
		if strings.Contains(strings.Join(call.args, " "), hostile) {
			t.Fatalf("token in argv: %v", call.args)
		}
	}
	// The child environment is the daemon's own plus GH_PROMPT_DISABLED only.
	base := map[string]bool{}
	for _, e := range os.Environ() {
		base[e] = true
	}
	var added []string
	for _, e := range ghEnv() {
		if !base[e] {
			added = append(added, e)
		}
	}
	if len(added) != 1 || added[0] != "GH_PROMPT_DISABLED=1" {
		t.Fatalf("ghEnv adds %v", added)
	}
}

func TestPRDetails_ErrorsAreTyped(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return repoViewJSON, nil
		}
		return "", fmt.Errorf("gh: exit status 1: To get started with GitHub CLI, please run:  gh auth login")
	}}
	c := New(WithRunner(s.run))
	_, err := c.PRDetails(context.Background(), "/w", 5)
	fe, ok := err.(*forge.Error)
	if !ok || fe.Code != forge.CodeUnauthenticated {
		t.Fatalf("err = %v", err)
	}
	if n := len(s.callsWith("api")); n != 0 {
		t.Fatalf("graphql ran after the view failed: %d", n)
	}
}

func TestCheckLog_TailBounded(t *testing.T) {
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, fmt.Sprintf("line %03d \x00end", i))
	}
	s := &stubRunner{fn: func(call stubCall) (string, error) { return strings.Join(lines, "\n") + "\n", nil }}
	c := New(WithRunner(s.run))
	out, err := c.CheckLog(context.Background(), "/w", 111, 222)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.calls[0].args, " "); got != "run view 111 --job 222 --log-failed" {
		t.Fatalf("argv = %q", got)
	}
	if !strings.HasPrefix(out, forge.LogCutMarker+"\n") {
		t.Fatalf("no cut marker: %q", out[:60])
	}
	body := strings.TrimPrefix(out, forge.LogCutMarker+"\n")
	got := strings.Split(body, "\n")
	if len(got) != 200 || got[0] != "line 300 end" || got[199] != "line 499 end" || strings.ContainsRune(body, 0) {
		t.Fatalf("tail = %d lines, first %q last %q", len(got), got[0], got[len(got)-1])
	}

	// Byte bound, on a rune boundary: few lines, one huge.
	big := strings.Repeat("é", 20000)
	s = &stubRunner{fn: func(call stubCall) (string, error) { return "head\n" + big, nil }}
	c = New(WithRunner(s.run))
	out, _ = c.CheckLog(context.Background(), "/w", 5, 0)
	body = strings.TrimPrefix(out, forge.LogCutMarker+"\n")
	if len(body) > forge.CheckLogMaxBytes || strings.ToValidUTF8(body, "") != body || !strings.HasPrefix(out, forge.LogCutMarker) {
		t.Fatalf("byte-bounded tail = %d bytes", len(body))
	}
	if got := strings.Join(s.calls[0].args, " "); got != "run view 5 --log-failed" {
		t.Fatalf("argv without job = %q", got)
	}

	// Short log: untouched, no marker.
	s = &stubRunner{fn: func(call stubCall) (string, error) { return "a\nb\n", nil }}
	out, _ = New(WithRunner(s.run)).CheckLog(context.Background(), "/w", 5, 0)
	if out != "a\nb" {
		t.Fatalf("short log = %q", out)
	}
	if _, err := c.CheckLog(context.Background(), "/w", 0, 0); err == nil {
		t.Fatal("run id 0 accepted")
	}
}
