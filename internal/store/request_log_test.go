package store

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func newTestRequestLog(t *testing.T, s *Store, startedAt time.Time, accountID *int64, model *string, input, output int64) RequestLog {
	t.Helper()
	r, err := s.CreateRequestLog(RequestLog{
		StartedAt:   startedAt,
		Provider:    "anthropic",
		AccountID:   accountID,
		SessionKey:  "sess",
		Model:       model,
		Stream:      ptr(false),
		Status:      200,
		Outcome:     RequestLogOutcomeOK,
		DurationMs:  10,
		InputTokens: ptr(input), OutputTokens: ptr(output),
	})
	if err != nil {
		t.Fatalf("CreateRequestLog() error = %v", err)
	}
	return r
}

func TestStore_CreateAndListRequestLog(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	acct := newTestAccount(t, s, "anthropic", "a1")
	created := newTestRequestLog(t, s, time.Now(), &acct.ID, ptr("claude-3"), 100, 50)

	if created.ID == 0 {
		t.Fatal("CreateRequestLog() did not assign an id")
	}

	rows, err := s.ListRequestLogs(RequestLogFilter{})
	if err != nil {
		t.Fatalf("ListRequestLogs() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListRequestLogs() = %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.AccountID == nil || *got.AccountID != acct.ID {
		t.Errorf("AccountID = %v, want %d", got.AccountID, acct.ID)
	}
	if got.Model == nil || *got.Model != "claude-3" {
		t.Errorf("Model = %v, want claude-3", got.Model)
	}
	if got.Stream == nil || *got.Stream != false {
		t.Errorf("Stream = %v, want false", got.Stream)
	}
	if got.InputTokens == nil || *got.InputTokens != 100 {
		t.Errorf("InputTokens = %v, want 100", got.InputTokens)
	}
}

// TestStore_RequestLogNullableFields exercises RequestLog's "nil means
// unknown" convention: a route-failure row has no account, no model/stream
// (the request body was never even parsed), and no tokens.
func TestStore_RequestLogNullableFields(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	created, err := s.CreateRequestLog(RequestLog{
		StartedAt:  time.Now(),
		Provider:   "anthropic",
		SessionKey: "sess",
		Status:     503,
		Outcome:    RequestLogOutcomeRouteError,
		Error:      ptr("no anthropic accounts configured"),
		DurationMs: 1,
	})
	if err != nil {
		t.Fatalf("CreateRequestLog() error = %v", err)
	}

	got, err := s.ListRequestLogs(RequestLogFilter{})
	if err != nil {
		t.Fatalf("ListRequestLogs() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListRequestLogs() = %d rows, want 1", len(got))
	}
	r := got[0]
	if r.ID != created.ID {
		t.Fatalf("ID = %d, want %d", r.ID, created.ID)
	}
	if r.AccountID != nil {
		t.Errorf("AccountID = %v, want nil", r.AccountID)
	}
	if r.Model != nil {
		t.Errorf("Model = %v, want nil", r.Model)
	}
	if r.Stream != nil {
		t.Errorf("Stream = %v, want nil", r.Stream)
	}
	if r.InputTokens != nil || r.OutputTokens != nil {
		t.Errorf("InputTokens/OutputTokens = %v/%v, want nil", r.InputTokens, r.OutputTokens)
	}
	if r.UpstreamStatus != nil {
		t.Errorf("UpstreamStatus = %v, want nil", r.UpstreamStatus)
	}
}

// TestStore_SummarizeRequestLogs_GroupByAccount covers the M1 scenario
// "usage.summary groupBy=account sums match the inserted rows; since is
// inclusive and until is exclusive."
func TestStore_SummarizeRequestLogs_GroupByAccount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a1 := newTestAccount(t, s, "anthropic", "a1")
	a2 := newTestAccount(t, s, "anthropic", "a2")

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	newTestRequestLog(t, s, base, &a1.ID, ptr("claude-3"), 100, 10)
	newTestRequestLog(t, s, base.Add(time.Minute), &a1.ID, ptr("claude-3"), 200, 20)
	newTestRequestLog(t, s, base.Add(2*time.Minute), &a2.ID, ptr("claude-3"), 5, 1)
	// Outside the [since, until) window below: excluded from the sums.
	newTestRequestLog(t, s, base.Add(-time.Hour), &a1.ID, ptr("claude-3"), 999, 999)
	newTestRequestLog(t, s, base.Add(time.Hour), &a1.ID, ptr("claude-3"), 999, 999)

	since := base                      // inclusive: base itself must be included
	until := base.Add(3 * time.Minute) // exclusive: rows at/after this are excluded

	summary, err := s.SummarizeRequestLogs(&since, &until, "account")
	if err != nil {
		t.Fatalf("SummarizeRequestLogs() error = %v", err)
	}
	if len(summary) != 2 {
		t.Fatalf("SummarizeRequestLogs() = %d groups, want 2; got %+v", len(summary), summary)
	}

	byKey := map[string]UsageSummary{}
	for _, u := range summary {
		byKey[u.Key] = u
	}

	a1Key := formatAccountKey(a1.ID)
	a2Key := formatAccountKey(a2.ID)

	got1, ok := byKey[a1Key]
	if !ok {
		t.Fatalf("no summary group for account %d; got %+v", a1.ID, summary)
	}
	if got1.Count != 2 || got1.InputTokens != 300 || got1.OutputTokens != 30 {
		t.Errorf("account %d summary = %+v, want count=2 input=300 output=30", a1.ID, got1)
	}

	got2, ok := byKey[a2Key]
	if !ok {
		t.Fatalf("no summary group for account %d; got %+v", a2.ID, summary)
	}
	if got2.Count != 1 || got2.InputTokens != 5 || got2.OutputTokens != 1 {
		t.Errorf("account %d summary = %+v, want count=1 input=5 output=1", a2.ID, got2)
	}
}

func TestStore_SummarizeRequestLogs_InvalidGroupBy(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if _, err := s.SummarizeRequestLogs(nil, nil, "bogus"); err == nil {
		t.Fatal("SummarizeRequestLogs(groupBy=bogus) succeeded, want error")
	}
}
