package wsapi

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/accounts"
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
