package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/ghctx"
)

// forgeTestServer serves a real API whose gh client runs stub instead of the
// real gh binary.
func forgeTestServer(t *testing.T, stub ghctx.Runner) (*httptest.Server, int64) {
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

func TestForgeSearchRPC_ReturnsItems(t *testing.T) {
	t.Parallel()
	srv, taskID := forgeTestServer(t, func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return `[{"number":7,"title":"Fix it","state":"OPEN","author":{"login":"ann"},"url":"https://github.com/o/r/pull/7","updatedAt":"2026-10-22T00:00:00Z","headRefName":"fix"}]`, nil
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"taskId": taskID, "kind": "pr", "query": "fix"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("forge.search error = %v", resp.Error.Message)
	}
	var got forgeSearchResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.Forge != "github" || got.Repo != "o/r" || len(got.Items) != 1 || got.Items[0].Number != 7 || got.Items[0].Author != "ann" || got.Items[0].State != "open" {
		t.Fatalf("result = %+v", got)
	}
}

func TestForgeSearchRPC_TypedErrorCode(t *testing.T) {
	t.Parallel()
	srv, taskID := forgeTestServer(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("gh: To get started with GitHub CLI, please run:  gh auth login")
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"taskId": taskID, "kind": "issue"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil || resp.Error.Code != forge.CodeUnauthenticated || !strings.Contains(resp.Error.Message, "gh auth login") {
		t.Fatalf("error = %+v, want code %q mentioning gh auth login", resp.Error, forge.CodeUnauthenticated)
	}

	sendRequest(t, ws, "2", "forge.search", map[string]any{"taskId": taskID, "kind": "bogus"})
	resp = readEnvelopeFor(t, ws, "2", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "kind") {
		t.Fatalf("bad kind error = %+v", resp.Error)
	}
}

func TestForgeSearchRPC_RejectsOtherForges(t *testing.T) {
	t.Parallel()
	srv, taskID := forgeTestServer(t, func(context.Context, string, ...string) (string, error) {
		t.Error("runner must not run for an unsupported forge")
		return "", nil
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"forge": "gitlab", "taskId": taskID, "kind": "pr"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil || !strings.Contains(resp.Error.Message, "unsupported forge") {
		t.Fatalf("error = %+v, want unsupported forge", resp.Error)
	}
}

func TestForgeSearch_ErrorCarriesCodeAndDetail(t *testing.T) {
	t.Parallel()
	srv, taskID := forgeTestServer(t, func(context.Context, string, ...string) (string, error) {
		return "", errors.New("gh: exit status 1: HTTP 404: Not Found secretquery-ghp_" + strings.Repeat("k", 30))
	})
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"taskId": taskID, "kind": "pr"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil || resp.Error.Code != forge.CodeNotFound {
		t.Fatalf("error = %+v, want code %q", resp.Error, forge.CodeNotFound)
	}
	data, ok := resp.Error.Data.(map[string]any)
	if !ok {
		t.Fatalf("error data = %#v, want an object", resp.Error.Data)
	}
	detail, _ := data["detail"].(string)
	if !strings.Contains(detail, "404") || strings.Contains(detail, "ghp_") {
		t.Fatalf("detail = %q, want scrubbed stderr", detail)
	}
	if strings.Contains(resp.Error.Message, "404") {
		t.Fatalf("message must be the fixed sentence, got %q", resp.Error.Message)
	}
}

// forgeWorkspaceServer is forgeTestServer plus the workspace id and path the
// test task lives in, and the dir each gh call ran in.
func forgeWorkspaceServer(t *testing.T) (srv *httptest.Server, taskID, workspaceID int64, wsPath string, dirs *[]string) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	w, err := wm.GetWorkspace(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(wm, newTestAccountsRegistry(t), newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(api.Close)
	var mu sync.Mutex
	dirs = &[]string{}
	api.SetGithubClient(ghctx.New(ghctx.WithRunner(func(_ context.Context, dir string, args ...string) (string, error) {
		mu.Lock()
		*dirs = append(*dirs, dir)
		mu.Unlock()
		if args[0] == "repo" {
			return `{"nameWithOwner":"o/r"}`, nil
		}
		return `[{"number":9,"title":"From fork","state":"OPEN","author":{"login":"zed"},"url":"https://github.com/o/r/pull/9","updatedAt":"2026-10-22T00:00:00Z","headRefName":"fork-branch","isCrossRepository":true}]`, nil
	})))
	srv = httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return srv, task.ID, w.ID, w.Path, dirs
}

func TestForgeSearch_ByWorkspaceID(t *testing.T) {
	t.Parallel()
	srv, _, wsID, wsPath, dirs := forgeWorkspaceServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"workspaceId": wsID, "kind": "pr"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("forge.search error = %v", resp.Error.Message)
	}
	var got forgeSearchResult
	if err := json.Unmarshal(resp.Result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Forge != "github" || got.Repo != "o/r" || len(got.Items) != 1 || got.Items[0].Number != 9 || !got.Items[0].FromFork {
		t.Fatalf("result = %+v", got)
	}
	if len(*dirs) == 0 {
		t.Fatal("gh never ran")
	}
	for _, d := range *dirs {
		if d != wsPath {
			t.Fatalf("gh ran in %q, want the workspace path %q", d, wsPath)
		}
	}
}

func TestForgeSearch_RequiresExactlyOneScope(t *testing.T) {
	t.Parallel()
	srv, taskID, wsID, _, dirs := forgeWorkspaceServer(t)
	ws := dialWS(t, srv, "tok")

	for i, params := range []map[string]any{
		{"kind": "pr"},
		{"kind": "pr", "taskId": taskID, "workspaceId": wsID},
	} {
		id := strconv.Itoa(i)
		sendRequest(t, ws, id, "forge.search", params)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error == nil || !strings.Contains(resp.Error.Message, "invalid params: pass exactly one of taskId or workspaceId") {
			t.Fatalf("case %d error = %+v, want the exactly-one sentence", i, resp.Error)
		}
	}
	if len(*dirs) != 0 {
		t.Fatalf("gh ran %d times for a rejected request", len(*dirs))
	}

	// taskId alone still works, in the task's worktree.
	sendRequest(t, ws, "ok", "forge.search", map[string]any{"kind": "pr", "taskId": taskID})
	if resp := readEnvelopeFor(t, ws, "ok", 5*time.Second); resp.Error != nil {
		t.Fatalf("taskId-only error = %v", resp.Error.Message)
	}
}

func TestForgeSearch_WorkspaceIDUnknown(t *testing.T) {
	t.Parallel()
	srv, _, _, _, dirs := forgeWorkspaceServer(t)
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "forge.search", map[string]any{"workspaceId": 999999, "kind": "pr"})
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error == nil || !strings.HasPrefix(resp.Error.Message, "forge.search: ") {
		t.Fatalf("error = %+v, want a forge.search-prefixed not-found error", resp.Error)
	}
	if len(*dirs) != 0 {
		t.Fatal("gh ran for an unknown workspace")
	}
}
