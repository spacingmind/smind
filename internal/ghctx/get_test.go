package ghctx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
)

// stubRunner is a Runner that records every invocation and answers from a
// scripted function.
type stubRunner struct {
	calls []stubCall
	fn    func(call stubCall) (string, error)
}

type stubCall struct {
	dir  string
	args []string
}

func (s *stubRunner) run(_ context.Context, dir string, args ...string) (string, error) {
	s.calls = append(s.calls, stubCall{dir: dir, args: args})
	return s.fn(stubCall{dir: dir, args: args})
}

func (s *stubRunner) callsWith(first string) []stubCall {
	var out []stubCall
	for _, c := range s.calls {
		if len(c.args) > 0 && c.args[0] == first {
			out = append(out, c)
		}
	}
	return out
}

func prViewJSON(n int) string {
	return fmt.Sprintf(`{"number":%d,"title":"Fix bug","url":"https://github.com/o/r/pull/%d","state":"OPEN","body":"body text","baseRefName":"main","headRefName":"fix","author":{"login":"alice"}}`, n, n)
}

// TestGithubAttach_RepoRegexAndArgvOnly asserts Get's gh argv: --repo is
// present only when the caller passed a repo, and a repo like "a/b;rm -rf"
// is rejected before the runner runs at all.
func TestGithubAttach_RepoRegexAndArgvOnly(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return prViewJSON(7), nil
	}}
	c := New(WithRunner(s.run))

	if _, err := c.Get(context.Background(), t.TempDir(), "pr", 7, ""); err != nil {
		t.Fatalf("Get with inferred repo: %v", err)
	}
	view := s.callsWith("pr")
	if len(view) != 1 {
		t.Fatalf("pr view calls = %d, want 1: %+v", len(view), s.calls)
	}
	wantArgs := []string{"pr", "view", "7", "--json", "number,title,url,state,body,baseRefName,headRefName"}
	if strings.Join(view[0].args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("argv = %v, want %v (no --repo)", view[0].args, wantArgs)
	}

	s.calls = nil
	if _, err := c.Get(context.Background(), t.TempDir(), "pr", 8, "owner/repo"); err != nil {
		t.Fatalf("Get with explicit repo: %v", err)
	}
	view = s.callsWith("pr")
	if len(view) != 1 {
		t.Fatalf("pr view calls = %d, want 1 (explicit repo must not run repo view): %+v", len(view), s.calls)
	}
	if got := view[0].args; !containsPair(got, "--repo", "owner/repo") {
		t.Fatalf("argv = %v, want --repo owner/repo", got)
	}

	// Malformed repos are rejected before any gh invocation.
	s.calls = nil
	for _, bad := range []string{"a/b;rm -rf", "a/b c", "/x", "a/", "owner/repo/extra", strings.Repeat("a", 101) + "/b"} {
		if _, err := c.Get(context.Background(), t.TempDir(), "pr", 1, bad); err == nil {
			t.Fatalf("Get(repo=%q) error = nil, want invalid-arguments error", bad)
		}
	}
	if len(s.calls) != 0 {
		t.Fatalf("runner ran %d time(s) for invalid repos, want 0: %+v", len(s.calls), s.calls)
	}

	// number and kind are validated too, also pre-runner.
	if _, err := c.Get(context.Background(), t.TempDir(), "pr", 0, ""); err == nil {
		t.Fatal("Get(number=0) error = nil, want error")
	}
	if _, err := c.Get(context.Background(), t.TempDir(), "wiki", 1, ""); err == nil {
		t.Fatal("Get(kind=wiki) error = nil, want error")
	}
	if len(s.calls) != 0 {
		t.Fatalf("runner ran %d time(s) for invalid args, want 0", len(s.calls))
	}
}

func containsPair(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}

func TestGet_ParsesSnapshot(t *testing.T) {
	s := &stubRunner{fn: func(call stubCall) (string, error) {
		if call.args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return prViewJSON(42), nil
	}}
	c := New(WithRunner(s.run))
	snap, err := c.Get(context.Background(), "/w", "pr", 42, "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if snap.Number != 42 || snap.Repo != "o/r" || snap.Title != "Fix bug" || snap.URL != "https://github.com/o/r/pull/42" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.State != "open" {
		t.Fatalf("state = %q, want lowercased %q", snap.State, "open")
	}
	if snap.BaseRef != "main" || snap.HeadRef != "fix" || snap.Body != "body text" {
		t.Fatalf("snapshot refs/body = %+v", snap)
	}

	// issue view uses the issue --json set (no ref fields).
	s.calls = nil
	s.fn = func(call stubCall) (string, error) {
		return `{"number":9,"title":"Bug","url":"u","state":"closed","body":"b","author":{"login":"bob"}}`, nil
	}
	snap, err = c.Get(context.Background(), "/w", "issue", 9, "o/r")
	if err != nil {
		t.Fatalf("Get(issue): %v", err)
	}
	view := s.callsWith("issue")
	if len(view) != 1 || strings.Join(view[0].args, " ") != "issue view 9 --json number,title,url,state,body --repo o/r" {
		t.Fatalf("issue argv = %+v", view)
	}
	if snap.State != "closed" || snap.BaseRef != "" || snap.HeadRef != "" {
		t.Fatalf("issue snapshot = %+v", snap)
	}
}

// TestGithubAttach_Timeout proves Get surfaces an error (not a hang) when
// its runner blocks on ctx: the runner returns as soon as the caller's
// short ctx deadline fires.
func TestGithubAttach_Timeout(t *testing.T) {
	blocking := func(ctx context.Context, _ string, _ ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	c := New(WithRunner(blocking))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(ctx, "/w", "pr", 1, "o/r")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Get error = nil, want an error from the cancelled ctx")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Get did not return after ctx deadline")
	}
}

// Compile-time: forge.Snapshot JSON round-trip sanity for the fields handlers
// serialize.
func TestSnapshotJSONFields(t *testing.T) {
	b, err := json.Marshal(forge.Snapshot{Number: 1, Repo: "o/r", Title: "t", URL: "u", State: "open", BaseRef: "main", HeadRef: "fix", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Number":1`, `"Repo":"o/r"`, `"State":"open"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("json = %s, missing %s", b, want)
		}
	}
}
