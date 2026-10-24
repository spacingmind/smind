package ghctx

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

const repoViewJSON = `{"nameWithOwner":"spacingmind/smind","url":"https://github.com/spacingmind/smind"}`

func fixtureOpenPRs(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/openprs.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func prStub(t *testing.T, list string) *stubRunner {
	return &stubRunner{fn: func(call stubCall) (string, error) {
		switch call.args[0] {
		case "repo":
			return repoViewJSON, nil
		case "pr":
			if call.args[1] == "view" {
				return `{"number":5,"url":"https://github.com/spacingmind/smind/pull/5","state":"MERGED","headRefName":"b","statusCheckRollup":[{"__typename":"StatusContext","state":"SUCCESS"}]}`, nil
			}
			return list, nil
		}
		t.Fatalf("unexpected gh call %v", call.args)
		return "", nil
	}}
}

func TestGhctx_OpenPRs_ArgvAndFields(t *testing.T) {
	s := prStub(t, fixtureOpenPRs(t))
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	c := New(WithRunner(s.run), WithNow(func() time.Time { return now }))
	prs, err := c.OpenPRs(context.Background(), "/w")
	if err != nil {
		t.Fatalf("OpenPRs: %v", err)
	}
	list := s.callsWith("pr")
	if len(list) != 1 || list[0].dir != "/w" {
		t.Fatalf("pr calls = %+v", list)
	}
	want := []string{"pr", "list", "--state", "open", "--limit", "100", "--json", prFields}
	if strings.Join(list[0].args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %v, want %v", list[0].args, want)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d PRs, want 3", len(prs))
	}
	p := prs[0]
	if p.Number != 487 || p.Repo != "spacingmind/smind" || p.URL != "https://github.com/spacingmind/smind/pull/487" ||
		p.State != "open" || p.Draft || p.BaseRef != "develop" || p.FromFork || p.Mergeable != "mergeable" ||
		p.ReviewDecision != "none" || !p.FetchedAt.Equal(now) || p.Stale != "" ||
		len(p.HeadSha) != 40 || !strings.HasPrefix(p.HeadRef, "smind/task-") {
		t.Fatalf("PR 487 = %+v", p)
	}
	if p.Checks == nil || *p.Checks != (forge.Checks{State: "pending", Passed: 2, Failed: 0, Pending: 2, Total: 4}) {
		t.Fatalf("PR 487 checks = %+v", p.Checks)
	}
	if prs[2].Number != 477 || prs[2].Checks == nil || prs[2].Checks.State != "none" {
		t.Fatalf("PR 477 = %+v checks %+v", prs[2], prs[2].Checks)
	}
	// No result cache: a second call runs gh again (the repo lookup is not repeated).
	if _, err := c.OpenPRs(context.Background(), "/w"); err != nil {
		t.Fatal(err)
	}
	if n := len(s.callsWith("pr")); n != 2 {
		t.Fatalf("pr calls after second OpenPRs = %d, want 2 (no result cache)", n)
	}
	if n := len(s.callsWith("repo")); n != 1 {
		t.Fatalf("repo calls = %d, want 1", n)
	}
}

func TestGhctx_OpenPRs_DropsInvalidURL(t *testing.T) {
	list := `[
	 {"number":1,"url":"javascript:alert(1)","state":"OPEN"},
	 {"number":2,"url":"https://evil.example.com/spacingmind/smind/pull/2","state":"OPEN"},
	 {"number":3,"url":"https://github.com/spacingmind/smind/pull/3?x=1#y","state":"OPEN"},
	 {"number":0,"url":"https://github.com/spacingmind/smind/pull/4","state":"OPEN"},
	 {"number":5},
	 null]`
	c := New(WithRunner(prStub(t, list).run))
	prs, err := c.OpenPRs(context.Background(), "/w")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 || prs[0].Number != 3 || prs[0].URL != "https://github.com/spacingmind/smind/pull/3" {
		t.Fatalf("prs = %+v, want only #3 with a canonical url", prs)
	}
}

func TestGhctx_PR_ArgvAndFields(t *testing.T) {
	s := prStub(t, "[]")
	c := New(WithRunner(s.run))
	pr, err := c.PR(context.Background(), "/w", 5)
	if err != nil {
		t.Fatalf("PR: %v", err)
	}
	calls := s.callsWith("pr")
	want := []string{"pr", "view", "5", "--json", prFields}
	if len(calls) != 1 || strings.Join(calls[0].args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("pr calls = %+v, want argv %v", calls, want)
	}
	if pr.Number != 5 || pr.State != "merged" || pr.Repo != "spacingmind/smind" || pr.Checks == nil || pr.Checks.State != "success" {
		t.Fatalf("PR = %+v", pr)
	}
	if _, err := c.PR(context.Background(), "/w", 0); err == nil {
		t.Fatal("PR(0) succeeded")
	}
	if len(s.callsWith("pr")) != 1 {
		t.Fatal("PR(0) ran gh")
	}
}

func TestGhctx_PR_NotFoundIsTyped(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return repoViewJSON, nil
		}
		return "", errors.New("gh: Could not resolve to a PullRequest with the number 9")
	}}
	_, err := New(WithRunner(s.run)).PR(context.Background(), "/w", 9)
	var fe *forge.Error
	if !errors.As(err, &fe) || fe.Code != forge.CodeNotFound {
		t.Fatalf("err = %v, want forge_not_found", err)
	}
}

func TestGhctx_Repo_HostAndCache(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		return `{"nameWithOwner":"o/r","url":"https://GHE.Example.com/o/r"}`, nil
	}}
	c := New(WithRunner(s.run))
	r, err := c.Repo(context.Background(), "/w")
	if err != nil || r != (forge.Repo{Host: "ghe.example.com", NameWithOwner: "o/r"}) {
		t.Fatalf("Repo = %+v, %v", r, err)
	}
	if got := strings.Join(s.calls[0].args, " "); got != "repo view --json nameWithOwner,url" {
		t.Fatalf("argv = %q", got)
	}
	if _, err := c.Repo(context.Background(), "/w"); err != nil || len(s.calls) != 1 {
		t.Fatalf("second Repo within 60s ran gh again (%d calls, err %v)", len(s.calls), err)
	}
	// A repo answer without a url has no host to validate PR urls against.
	s2 := &stubRunner{fn: func(stubCall) (string, error) { return `{"nameWithOwner":"o/r"}`, nil }}
	if _, err := New(WithRunner(s2.run)).Repo(context.Background(), "/w"); err == nil {
		t.Fatal("Repo without url succeeded")
	}
}

func TestGhctx_OpenPRs_SingleFlight(t *testing.T) {
	var lists atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	run := func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "repo" {
			return repoViewJSON, nil
		}
		lists.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return fixtureOpenPRs(t), nil
	}
	c := New(WithRunner(run))
	const n = 8
	var wg sync.WaitGroup
	results := make([][]forge.PR, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prs, err := c.OpenPRs(context.Background(), "/w")
			if err != nil {
				t.Errorf("OpenPRs: %v", err)
			}
			results[i] = prs
		}(i)
	}
	<-started
	time.Sleep(50 * time.Millisecond) // let the other callers park on the flight
	close(release)
	wg.Wait()
	if got := lists.Load(); got != 1 {
		t.Fatalf("gh pr list ran %d times for %d concurrent callers, want 1", got, n)
	}
	// Waiters get independent copies of the Checks pointers.
	results[0][0].Checks.Passed = 99
	if results[1][0].Checks.Passed == 99 {
		t.Fatal("callers share a *Checks")
	}
	// A different dir is a different flight.
	if _, err := c.OpenPRs(context.Background(), "/other"); err != nil {
		t.Fatal(err)
	}
	if lists.Load() != 2 {
		t.Fatalf("second dir did not call gh")
	}
}

func TestPRMapping_ChecksReviewMergeable_Table(t *testing.T) {
	for in, want := range map[string]string{"OPEN": "open", "CLOSED": "closed", "MERGED": "merged", "open": "open", "": "open", "WAT": "open"} {
		if got := mapPRState(in); got != want {
			t.Errorf("mapPRState(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"MERGEABLE": "mergeable", "CONFLICTING": "conflicting", "UNKNOWN": "unknown", "": "unknown", "x": "unknown"} {
		if got := mapMergeable(in); got != want {
			t.Errorf("mapMergeable(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes_requested", "REVIEW_REQUIRED": "review_required", "": "none", "x": "none"} {
		if got := mapReviewDecision(in); got != want {
			t.Errorf("mapReviewDecision(%q) = %q, want %q", in, got, want)
		}
	}

	run := func(status, concl string) ghCheckJSON {
		return ghCheckJSON{Typename: "CheckRun", Status: status, Conclusion: concl}
	}
	sc := func(state string) ghCheckJSON { return ghCheckJSON{Typename: "StatusContext", State: state} }
	cases := []struct {
		name  string
		items []ghCheckJSON
		want  forge.Checks
	}{
		{"empty", nil, forge.Checks{State: "none"}},
		{"all green", []ghCheckJSON{run("COMPLETED", "SUCCESS"), run("COMPLETED", "NEUTRAL"), run("COMPLETED", "SKIPPED"), sc("SUCCESS")}, forge.Checks{State: "success", Passed: 4, Total: 4}},
		{"in progress", []ghCheckJSON{run("IN_PROGRESS", ""), run("QUEUED", ""), run("COMPLETED", "SUCCESS")}, forge.Checks{State: "pending", Passed: 1, Pending: 2, Total: 3}},
		{"failure beats pending", []ghCheckJSON{run("COMPLETED", "FAILURE"), run("IN_PROGRESS", ""), run("COMPLETED", "SUCCESS")}, forge.Checks{State: "failure", Passed: 1, Failed: 1, Pending: 1, Total: 3}},
		{"every failed conclusion", []ghCheckJSON{run("COMPLETED", "FAILURE"), run("COMPLETED", "TIMED_OUT"), run("COMPLETED", "CANCELLED"), run("COMPLETED", "ACTION_REQUIRED"), run("COMPLETED", "STARTUP_FAILURE")}, forge.Checks{State: "failure", Failed: 5, Total: 5}},
		{"completed without conclusion", []ghCheckJSON{run("COMPLETED", ""), run("COMPLETED", "STALE")}, forge.Checks{State: "pending", Pending: 2, Total: 2}},
		{"status contexts", []ghCheckJSON{sc("SUCCESS"), sc("FAILURE"), sc("ERROR"), sc("PENDING"), sc("EXPECTED")}, forge.Checks{State: "failure", Passed: 1, Failed: 2, Pending: 2, Total: 5}},
		{"pending context only", []ghCheckJSON{sc("PENDING")}, forge.Checks{State: "pending", Pending: 1, Total: 1}},
		{"lowercase tolerated", []ghCheckJSON{run("completed", "success")}, forge.Checks{State: "success", Passed: 1, Total: 1}},
		{"untyped items classified by fields", []ghCheckJSON{{Status: "COMPLETED", Conclusion: "SUCCESS"}, {State: "FAILURE"}}, forge.Checks{State: "failure", Passed: 1, Failed: 1, Total: 2}},
		{"unknown items skipped", []ghCheckJSON{{Typename: "Mystery"}, {}}, forge.Checks{State: "none"}},
	}
	for _, tc := range cases {
		if got := rollupChecks(tc.items); got == nil || *got != tc.want {
			t.Errorf("%s: rollupChecks = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestPRMapping_FromForkAndMissingFields(t *testing.T) {
	repo := forge.Repo{Host: "github.com", NameWithOwner: "o/r"}
	pr, ok := mapPR(ghPRJSON{Number: 1, URL: "https://github.com/o/r/pull/1", IsCrossRepository: true, IsDraft: true}, repo, time.Time{})
	if !ok || !pr.FromFork || !pr.Draft || pr.Mergeable != "unknown" || pr.ReviewDecision != "none" || pr.State != "open" {
		t.Fatalf("pr = %+v ok=%v", pr, ok)
	}
}

func TestGhctx_PRsByHead_ArgvAndMapping(t *testing.T) {
	list := `[{"number":9,"url":"https://github.com/spacingmind/smind/pull/9","state":"MERGED","headRefName":"smind/task-x",` +
		`"headRefOid":"abc","isCrossRepository":true},{"number":4,"url":"https://evil.example/o/r/pull/4","state":"OPEN"}]`
	s := prStub(t, list)
	c := New(WithRunner(s.run))
	prs, err := c.PRsByHead(context.Background(), "/w", "smind/task-x")
	if err != nil {
		t.Fatalf("PRsByHead: %v", err)
	}
	calls := s.callsWith("pr")
	want := []string{"pr", "list", "--head", "smind/task-x", "--state", "all", "--limit", "5", "--json", prByHeadFields}
	if len(calls) != 1 || strings.Join(calls[0].args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("pr calls = %+v, want one with argv %v", calls, want)
	}
	if len(prs) != 1 || prs[0].Number != 9 || prs[0].State != "merged" || prs[0].HeadSha != "abc" || !prs[0].FromFork || prs[0].HeadRef != "smind/task-x" {
		t.Fatalf("prs = %+v, want PR 9 only (the off-host url is dropped)", prs)
	}
}

func TestGhctx_PRsByHead_RejectsAnOptionLikeBranch(t *testing.T) {
	s := prStub(t, "[]")
	c := New(WithRunner(s.run))
	for _, head := range []string{"", "--web", "-x", "a\nb"} {
		if _, err := c.PRsByHead(context.Background(), "/w", head); err == nil {
			t.Fatalf("PRsByHead(%q) = nil error, want invalid arguments", head)
		}
	}
	if n := len(s.callsWith("pr")) + len(s.callsWith("repo")); n != 0 {
		t.Fatalf("gh was called %d times for invalid heads", n)
	}
}
