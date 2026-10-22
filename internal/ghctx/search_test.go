package ghctx

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestGithubSearch_ArgvAndJSONFields asserts the pr/issue list argv and
// that gh's JSON fields land on Item (author is flattened from {login},
// state lowercased).
func TestGithubSearch_ArgvAndJSONFields(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return `[{"number":5,"title":"T","state":"OPEN","author":{"login":"alice"},"url":"u5","updatedAt":"2026-01-02T03:04:05Z","headRefName":"fix"},{"number":4,"title":"U","state":"open","author":{"login":"bob"},"url":"u4","updatedAt":"2026-01-03T03:04:05Z"}]`, nil
	}}
	c := New(WithRunner(s.run))
	res, err := c.Search(context.Background(), "/w", "pr", "", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Repo != "o/r" || len(res.Items) != 2 {
		t.Fatalf("res = %+v", res)
	}
	if res.Items[0].Number != 5 || res.Items[0].Author != "alice" || res.Items[0].State != "open" || res.Items[0].HeadRef != "fix" || res.Items[0].UpdatedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("items[0] = %+v", res.Items[0])
	}
	if res.Items[1].HeadRef != "" {
		t.Fatalf("items[1].HeadRef = %q, want empty (no headRefName in gh JSON)", res.Items[1].HeadRef)
	}
	list := s.callsWith("pr")
	if len(list) != 1 {
		t.Fatalf("pr list calls = %d, want 1", len(list))
	}
	if want := "pr list --state open --limit 30 --json number,title,state,author,url,updatedAt,headRefName"; strings.Join(list[0].args, " ") != want {
		t.Fatalf("argv = %v, want %q", list[0].args, want)
	}

	// Non-empty query appends --search; issue list uses issue fields and
	// clamps the limit.
	s.calls = nil
	if _, err := c.Search(context.Background(), "/w", "issue", "memory leak", 99); err != nil {
		t.Fatalf("Search(issue): %v", err)
	}
	list2 := s.callsWith("issue")
	if len(list2) != 1 {
		t.Fatalf("issue list calls = %d, want 1", len(list2))
	}
	if want := "issue list --state open --limit 30 --json number,title,state,author,url,updatedAt --search memory leak"; strings.Join(list2[0].args, " ") != want {
		t.Fatalf("argv = %v, want %q", list2[0].args, want)
	}
}

// TestGithubSearch_NumberAndURLResolve proves a numeric or URL query
// switches to pr/issue view (one-item result), kind from the URL path,
// and a not-found view yields empty Items with no error.
func TestGithubSearch_NumberAndURLResolve(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		if len(call.args) > 1 && call.args[1] == "view" {
			n := call.args[2]
			if n == "404" {
				return "", fmt.Errorf("gh: Could not resolve to a PullRequest with the number 404")
			}
			return prViewJSON(atoi(n)), nil
		}
		return `[]`, nil
	}}
	c := New(WithRunner(s.run))

	res, err := c.Search(context.Background(), "/w", "pr", "#123", 0)
	if err != nil {
		t.Fatalf("Search(#123): %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].Number != 123 || res.Repo != "o/r" {
		t.Fatalf("res = %+v, want one item #123 in o/r", res)
	}
	view := s.callsWith("pr")
	if len(view) != 1 || view[0].args[1] != "view" {
		t.Fatalf("argv = %+v, want pr view", view)
	}
	if containsFlag(view[0].args, "--repo") {
		t.Fatalf("bare-number resolve must not pass --repo: %v", view[0].args)
	}

	// URL query: kind and repo from the URL (issues -> issue), kind arg
	// overridden.
	s.calls = nil
	res, err = c.Search(context.Background(), "/w", "pr", "https://github.com/foo/bar.dev/issues/9", 0)
	if err != nil {
		t.Fatalf("Search(url): %v", err)
	}
	if len(res.Items) != 1 || res.Repo != "foo/bar.dev" {
		t.Fatalf("res = %+v, want one item in foo/bar.dev", res)
	}
	view = s.callsWith("issue")
	if len(view) != 1 || !containsPair(view[0].args, "--repo", "foo/bar.dev") {
		t.Fatalf("argv = %+v, want issue view --repo foo/bar.dev", view)
	}

	// Pull URL under a different owner.
	s.calls = nil
	if _, err := c.Search(context.Background(), "/w", "issue", "https://github.com/other/r/pull/3", 0); err != nil {
		t.Fatalf("Search(pull url): %v", err)
	}
	view = s.callsWith("pr")
	if len(view) != 1 || !containsPair(view[0].args, "--repo", "other/r") {
		t.Fatalf("argv = %+v, want pr view --repo other/r", view)
	}

	// Not-found resolves to empty Items, no error, no list fallback.
	s.calls = nil
	res, err = c.Search(context.Background(), "/w", "pr", "404", 0)
	if err != nil {
		t.Fatalf("Search(404) error = %v, want nil", err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("res.Items = %+v, want empty", res.Items)
	}
	if n := len(s.callsWith("pr")); n != 1 {
		t.Fatalf("pr calls after not-found = %d, want 1 (no list fallback): %+v", n, s.calls)
	}

	// Malformed URLs and non-numeric queries go through the list path.
	s.calls = nil
	if _, err := c.Search(context.Background(), "/w", "pr", "https://github.com/o/r/pull/abc", 0); err != nil {
		t.Fatalf("Search(bad url): %v", err)
	}
	if len(s.callsWith("pr")) != 1 || s.callsWith("pr")[0].args[1] != "list" {
		t.Fatalf("argv = %+v, want pr list for malformed URL query", s.callsWith("pr"))
	}
}

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func atoi(s string) int {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// TestGithubSearch_GhMissingTypedError: a missing gh binary surfaces as
// github_unavailable.
func TestGithubSearch_GhMissingTypedError(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return "", fmt.Errorf(`exec: "gh": executable file not found in $PATH`)
	}}
	c := New(WithRunner(s.run))
	_, err := c.Search(context.Background(), "/w", "pr", "", 0)
	te, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if te.Code != CodeUnavailable || te.Message != "gh is not installed on the daemon host" {
		t.Fatalf("err = %+v", te)
	}
}

// TestGithubSearch_NoGithubRemoteTypedError: a dir with no GitHub remote
// surfaces as github_unavailable with the no-remote message.
func TestGithubSearch_NoGithubRemoteTypedError(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		return "", fmt.Errorf("gh: none of the git remotes point to a known GitHub host")
	}}
	c := New(WithRunner(s.run))
	_, err := c.Search(context.Background(), "/w", "pr", "", 0)
	te, ok := err.(*Error)
	if !ok {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if te.Code != CodeUnavailable || te.Message != "this workspace has no GitHub remote" {
		t.Fatalf("err = %+v", te)
	}
}
