package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/ghctx"
)

// githubTestServer serves a real API whose gh client runs stub instead of the
// real gh binary.
func githubTestServer(t *testing.T, stub ghctx.Runner) (*httptest.Server, int64) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(stub)))
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return srv, task.ID
}

func TestGithubSearchRPC_ReturnsItems(t *testing.T) {
	t.Parallel()
	srv, taskID := githubTestServer(t, func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return `[{"number":7,"title":"Fix it","state":"OPEN","author":{"login":"ann"},"url":"https://github.com/o/r/pull/7","updatedAt":"2026-10-22T00:00:00Z","headRefName":"fix"}]`, nil
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "github.search", map[string]any{"taskId": taskID, "kind": "pr", "query": "fix"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("github.search error = %v", resp.Error.Message)
	}
	var got ghctx.SearchResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.Repo != "o/r" || len(got.Items) != 1 || got.Items[0].Number != 7 || got.Items[0].Author != "ann" || got.Items[0].State != "open" {
		t.Fatalf("result = %+v", got)
	}
}

func TestGithubSearchRPC_TypedErrorCode(t *testing.T) {
	t.Parallel()
	srv, taskID := githubTestServer(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("gh: To get started with GitHub CLI, please run:  gh auth login")
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "github.search", map[string]any{"taskId": taskID, "kind": "issue"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil || resp.Error.Code != ghctx.CodeUnavailable || !strings.Contains(resp.Error.Message, "gh auth login") {
		t.Fatalf("error = %+v, want code %q mentioning gh auth login", resp.Error, ghctx.CodeUnavailable)
	}

	sendRequest(t, ws, "2", "github.search", map[string]any{"taskId": taskID, "kind": "bogus"})
	resp = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "kind") {
		t.Fatalf("bad kind error = %+v", resp.Error)
	}
}
