package wsapi

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

// newUsageTestServer builds a server whose accounts registry shares the
// test's store: request_log.account_id has a foreign key into accounts(id)
// (see schema.sql), so usage tests must create a real account row before
// inserting request logs against it.
func newUsageTestServer(t *testing.T) (*httptest.Server, *accounts.Registry, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	wm := workspace.New(s)
	registry := accounts.New(s)
	srv := newTestWSServerWithAccounts(t, wm, registry, nil, s, "tok")
	return srv, registry, s
}

func ptr[T any](v T) *T { return &v }

// TestUsage_ListAndSummaryRoundTrip covers usage.list and usage.summary's
// wiring end to end over the wire protocol: insert rows directly (as
// internal/server's async writer would), then confirm both RPCs surface
// them correctly, including usage.summary's groupBy=account sums and
// since-inclusive/until-exclusive window (store package's own tests cover
// the aggregation SQL in more depth; this is the RPC layer on top).
func TestUsage_ListAndSummaryRoundTrip(t *testing.T) {
	t.Parallel()
	srv, registry, db := newUsageTestServer(t)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	acct, err := registry.AddAPIKey("anthropic", "a1", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mustCreateRequestLog(t, db, store.RequestLog{
		StartedAt: base, Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1",
		Model: ptr("claude-3"), Stream: ptr(false), Status: 200, Outcome: store.RequestLogOutcomeOK,
		DurationMs: 5, InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(20)),
	})
	mustCreateRequestLog(t, db, store.RequestLog{
		StartedAt: base.Add(time.Minute), Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1",
		Model: ptr("claude-3"), Stream: ptr(false), Status: 200, Outcome: store.RequestLogOutcomeOK,
		DurationMs: 5, InputTokens: ptr(int64(50)), OutputTokens: ptr(int64(10)),
	})
	// Outside the summary window used below: must not affect its sums.
	mustCreateRequestLog(t, db, store.RequestLog{
		StartedAt: base.Add(time.Hour), Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1",
		Status: 503, Outcome: store.RequestLogOutcomeRouteError, DurationMs: 1,
	})

	sendRequest(t, ws, "list", "usage.list", map[string]any{})
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("usage.list error = %v", resp.Error.Message)
	}
	var entries []usageLogEntry
	if err := json.Unmarshal(resp.Result, &entries); err != nil {
		t.Fatalf("decode usage.list result: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("usage.list returned %d entries, want 3", len(entries))
	}
	for _, e := range entries {
		if e.AccountID == nil || *e.AccountID != acct.ID {
			t.Errorf("entry AccountID = %v, want %d", e.AccountID, acct.ID)
		}
	}

	sendRequest(t, ws, "summary", "usage.summary", map[string]any{
		"since":   base.Format(time.RFC3339),
		"until":   base.Add(2 * time.Minute).Format(time.RFC3339),
		"groupBy": "account",
	})
	resp = readEnvelopeFor(t, ws, "summary", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("usage.summary error = %v", resp.Error.Message)
	}
	var summary []usageSummaryEntry
	if err := json.Unmarshal(resp.Result, &summary); err != nil {
		t.Fatalf("decode usage.summary result: %v", err)
	}
	if len(summary) != 1 {
		t.Fatalf("usage.summary returned %d groups, want 1; got %+v", len(summary), summary)
	}
	if summary[0].Count != 2 || summary[0].InputTokens != 150 || summary[0].OutputTokens != 30 {
		t.Errorf("summary = %+v, want count=2 input=150 output=30 (the route_error row at +1h excluded)", summary[0])
	}
}

func TestUsage_SummaryRequiresGroupBy(t *testing.T) {
	t.Parallel()
	srv, _, _ := newUsageTestServer(t)
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	sendRequest(t, ws, "summary", "usage.summary", map[string]any{})
	resp := readEnvelopeFor(t, ws, "summary", 5*time.Second)
	if resp.Error == nil {
		t.Fatal("usage.summary with no groupBy succeeded, want an error")
	}
}

func mustCreateRequestLog(t *testing.T, db *store.Store, r store.RequestLog) {
	t.Helper()
	if _, err := db.CreateRequestLog(r); err != nil {
		t.Fatalf("CreateRequestLog() error = %v", err)
	}
}

// seedRunUsage creates a workspace, tasks, runs and run_usage rows for the
// runs-scope tests: taskA has two runs (100/20 cost 0.5 and 50/NULL cost
// NULL, model m1), taskB one run (7/3, cost 0.25, model m2).
func seedRunUsage(t *testing.T, db *store.Store, base time.Time) {
	t.Helper()
	ws, err := db.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	taskA, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "a", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	taskB, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "b", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	for _, r := range []struct {
		id    string
		task  int64
		usage store.RunUsage
	}{
		{"ra1", taskA.ID, store.RunUsage{Model: ptr("m1"), InputTokens: ptr(int64(100)), OutputTokens: ptr(int64(20)), CostUSD: ptr(0.5)}},
		{"ra2", taskA.ID, store.RunUsage{Model: ptr("m1"), InputTokens: ptr(int64(50))}},
		{"rb1", taskB.ID, store.RunUsage{Model: ptr("m2"), InputTokens: ptr(int64(7)), OutputTokens: ptr(int64(3)), CostUSD: ptr(0.25)}},
	} {
		if _, err := db.CreateRun(store.Run{ID: r.id, TaskID: r.task, Provider: "glm", Prompt: "p", Status: "done", StartedAt: base}); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}
		r.usage.RunID, r.usage.TaskID, r.usage.Provider, r.usage.Source = r.id, r.task, "glm", "claude_result"
		if err := db.UpsertRunUsage(r.usage); err != nil {
			t.Fatalf("UpsertRunUsage() error = %v", err)
		}
	}
}

func callUsageSummary(t *testing.T, srv *httptest.Server, params map[string]any) ([]usageSummaryEntry, *envelope) {
	t.Helper()
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })
	sendRequest(t, ws, "s", "usage.summary", params)
	resp := readEnvelopeFor(t, ws, "s", 5*time.Second)
	if resp.Error != nil {
		return nil, &resp
	}
	var out []usageSummaryEntry
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		t.Fatalf("decode usage.summary result: %v", err)
	}
	return out, nil
}

// TestRunUsage_UpsertAndSummary (summary side): usage.summary scope=runs
// sums per task, scope=all merges proxy and run rows, and the scope/groupBy
// combinations validate.
func TestRunUsage_UpsertAndSummary(t *testing.T) {
	t.Parallel()
	srv, registry, db := newUsageTestServer(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seedRunUsage(t, db, base)
	acct, err := registry.AddAPIKey("anthropic", "a1", "sk-test")
	if err != nil {
		t.Fatalf("AddAPIKey() error = %v", err)
	}
	mustCreateRequestLog(t, db, store.RequestLog{
		StartedAt: base, Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1", Model: ptr("m1"),
		Status: 200, Outcome: store.RequestLogOutcomeOK, DurationMs: 5, InputTokens: ptr(int64(1000)),
	})

	rows, errEnv := callUsageSummary(t, srv, map[string]any{"scope": "runs", "groupBy": "task"})
	if errEnv != nil {
		t.Fatalf("scope=runs groupBy=task error = %v", errEnv.Error.Message)
	}
	if len(rows) != 2 {
		t.Fatalf("scope=runs groupBy=task = %+v, want 2 rows", rows)
	}
	a := rows[0]
	if a.Scope != "runs" || a.Count != 2 || a.InputTokens != 150 || a.OutputTokens != 20 ||
		a.CostUSD == nil || *a.CostUSD != 0.5 {
		t.Errorf("task A row = %+v, want runs count=2 in=150 out=20 cost=0.5", a)
	}

	rows, errEnv = callUsageSummary(t, srv, map[string]any{"scope": "all", "groupBy": "model"})
	if errEnv != nil {
		t.Fatalf("scope=all groupBy=model error = %v", errEnv.Error.Message)
	}
	var proxyRows, runRows int
	for _, r := range rows {
		switch r.Scope {
		case "proxy":
			proxyRows++
			if r.CostUSD != nil {
				t.Errorf("proxy row carries costUsd: %+v", r)
			}
		case "runs":
			runRows++
		}
	}
	if proxyRows != 1 || runRows != 2 {
		t.Errorf("scope=all groupBy=model = %+v, want 1 proxy + 2 runs rows", rows)
	}

	// Default scope is all; groupBy=account is proxy-only and keeps today's rows.
	rows, errEnv = callUsageSummary(t, srv, map[string]any{"groupBy": "account"})
	if errEnv != nil || len(rows) != 1 || rows[0].Scope != "proxy" || rows[0].InputTokens != 1000 {
		t.Errorf("default scope groupBy=account = %+v, %v; want one proxy row", rows, errEnv)
	}

	for name, params := range map[string]map[string]any{
		"unknown scope":          {"scope": "nope", "groupBy": "model"},
		"proxy with task":        {"scope": "proxy", "groupBy": "task"},
		"runs with account":      {"scope": "runs", "groupBy": "account"},
		"all with unknown":       {"scope": "all", "groupBy": "bogus"},
		"runs with day grouping": {"scope": "runs", "groupBy": "day"},
	} {
		if _, errEnv := callUsageSummary(t, srv, params); errEnv == nil {
			t.Errorf("%s: succeeded, want an error", name)
		}
	}
}

// TestRunList_CarriesUsageTotals: a finished run with a run_usage row shows
// camelCase totals under "usage" in run.list; one whose backend reported
// nothing shows only {"source":"not_reported"}. (A run that ends before turn
// end has no usage key at all: covered in internal/runs.)
func TestRunList_CarriesUsageTotals(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	tasks := []store.Task{
		newTestTask(t, wm, "usage in=100 out=20 used=50 size=1000 cost=0.5"),
		newTestTask(t, wm, ""),
	}
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")
	t.Cleanup(func() { _ = ws.Close() })

	var ids []string
	for i := 0; i < 2; i++ {
		sendRequest(t, ws, "start", "run.start", map[string]any{"taskId": tasks[i].ID, "provider": "glm", "prompt": "hi"})
		resp := readEnvelopeFor(t, ws, "start", 5*time.Second)
		var res runStartResult
		if err := json.Unmarshal(resp.Result, &res); err != nil || res.RunID == "" {
			t.Fatalf("run.start = %+v, %v", resp, err)
		}
		ids = append(ids, res.RunID)
		deadline := time.Now().Add(5 * time.Second)
		for {
			sendRequest(t, ws, "w", "run.list", nil)
			var list []runs.RunSummary
			_ = json.Unmarshal(readEnvelopeFor(t, ws, "w", 5*time.Second).Result, &list)
			done := false
			for _, s := range list {
				done = done || (s.ID == res.RunID && s.Status == runs.StatusDone)
			}
			if done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("run never finished")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	sendRequest(t, ws, "list", "run.list", nil)
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(resp.Result, &items); err != nil {
		t.Fatalf("decode run.list: %v", err)
	}
	for _, it := range items {
		var id string
		_ = json.Unmarshal(it["ID"], &id)
		raw, has := it["usage"]
		if id == ids[0] {
			var u map[string]any
			if err := json.Unmarshal(raw, &u); err != nil || u["inputTokens"] != float64(100) ||
				u["outputTokens"] != float64(20) || u["costUsd"] != 0.5 || u["source"] != "acp_prompt_usage" {
				t.Errorf("usage = %s, want camelCase 100/20/0.5", raw)
			}
			if _, ok := u["cachedInputTokens"]; ok {
				t.Errorf("unreported field present in usage: %s", raw)
			}
		} else if string(raw) != `{"source":"not_reported"}` {
			t.Errorf("run %s usage = %s (present %v), want only {\"source\":\"not_reported\"}", id, raw, has)
		}
	}
}
