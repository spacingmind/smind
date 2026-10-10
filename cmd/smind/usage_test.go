package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

// TestRunUsagePrintsSummaryRows drives `smind usage` end to end against a
// real wsapi.Handler-backed daemon: request-log rows seeded directly (as
// internal/server's async writer would) must surface as one aggregated
// row per account with summed tokens.
func TestRunUsagePrintsSummaryRows(t *testing.T) {
	_, s := newTestProfileDaemon(t)

	acct, err := s.CreateAccount(store.Account{
		Provider: "anthropic", Label: "a1", CredentialType: "api_key",
		CredentialData: `{"key":"sk-test"}`,
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	in, out := int64(100), int64(20)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, startedAt := range []time.Time{base, base.Add(time.Minute)} {
		if _, err := s.CreateRequestLog(store.RequestLog{
			StartedAt: startedAt, Provider: "anthropic", AccountID: &acct.ID, SessionKey: "s1",
			Model: strPtr("claude-3"), Stream: boolPtr(false), Status: 200, Outcome: store.RequestLogOutcomeOK,
			DurationMs: 5, InputTokens: &in, OutputTokens: &out,
		}); err != nil {
			t.Fatalf("CreateRequestLog(%d) error = %v", i, err)
		}
	}

	var code int
	stdout := captureStdout(t, func() { code = run([]string{"usage", "--by", "account"}) })
	if code != 0 {
		t.Fatalf("run(usage) = %d, want 0; stdout: %s", code, stdout)
	}
	key := strconv.FormatInt(acct.ID, 10)
	if !strings.Contains(stdout, key) {
		t.Fatalf("stdout = %q, want a row keyed by account %s", stdout, key)
	}
	if !strings.Contains(stdout, "200") || !strings.Contains(stdout, "40") {
		t.Fatalf("stdout = %q, want summed input=200 output=40", stdout)
	}
}

// TestRunUsageEmpty prints a friendly note rather than an empty table.
func TestRunUsageEmpty(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stdout := captureStdout(t, func() { code = run([]string{"usage"}) })
	if code != 0 {
		t.Fatalf("run(usage) with no rows = %d, want 0; stdout: %s", code, stdout)
	}
	if !strings.Contains(stdout, "no usage recorded") {
		t.Fatalf("stdout = %q, want the empty-usage notice", stdout)
	}
}

// TestRunUsageRejectsBadGroupBy guards the --by flag's allowed values.
func TestRunUsageRejectsBadGroupBy(t *testing.T) {
	newTestProfileDaemon(t)

	var code int
	stderr := captureStderr(t, func() { code = run([]string{"usage", "--by", "bogus"}) })
	if code == 0 {
		t.Fatalf("run(usage --by bogus) = 0, want 2; stderr: %s", stderr)
	}
}

// seedRunUsageRows creates a task with two finished runs carrying usage
// (one with no reported cost), as the Registry would have upserted.
func seedRunUsageRows(t *testing.T, s *store.Store) {
	t.Helper()
	ws, err := s.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	task, err := s.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	in, out, cost := int64(300), int64(40), 1.5
	for i, u := range []store.RunUsage{
		{InputTokens: &in, OutputTokens: &out, CostUSD: &cost},
		{InputTokens: &in},
	} {
		id := "run-" + strconv.Itoa(i)
		if _, err := s.CreateRun(store.Run{ID: id, TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now()}); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}
		u.RunID, u.TaskID, u.Provider, u.Source = id, task.ID, "glm", "claude_result"
		if err := s.UpsertRunUsage(u); err != nil {
			t.Fatalf("UpsertRunUsage() error = %v", err)
		}
	}
}

// TestRunUsageScopeRuns: --scope runs --by task prints a runs row with the
// summed tokens and cost under a SCOPE column; the default --by for
// --scope runs is task.
func TestRunUsageScopeRuns(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	seedRunUsageRows(t, s)

	for _, args := range [][]string{
		{"usage", "--scope", "runs", "--by", "task"},
		{"usage", "--scope", "runs"},
	} {
		var code int
		stdout := captureStdout(t, func() { code = run(args) })
		if code != 0 {
			t.Fatalf("run(%v) = %d; stdout: %s", args, code, stdout)
		}
		if !strings.Contains(stdout, "SCOPE") || !strings.Contains(stdout, "COST USD") {
			t.Errorf("%v: stdout = %q, want SCOPE and COST USD columns", args, stdout)
		}
		fields := strings.Fields(strings.Split(strings.TrimSpace(stdout), "\n")[1])
		// scope key count input output cacheRead cacheWrite reasoning cost
		if len(fields) != 9 || fields[0] != "runs" || fields[2] != "2" || fields[3] != "600" || fields[4] != "40" || fields[8] != "1.5" {
			t.Errorf("%v: row = %v, want runs/2 runs/600 in/40 out/1.5 cost", args, fields)
		}
	}
}

// TestRunUsageProxyRowShowsNoCost: a proxy row prints "-" for cost.
func TestRunUsageProxyRowShowsNoCost(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	in := int64(5)
	if _, err := s.CreateRequestLog(store.RequestLog{
		StartedAt: time.Now(), Provider: "anthropic", SessionKey: "s", Model: strPtr("m"),
		Status: 200, Outcome: store.RequestLogOutcomeOK, DurationMs: 1, InputTokens: &in,
	}); err != nil {
		t.Fatalf("CreateRequestLog() error = %v", err)
	}
	var code int
	stdout := captureStdout(t, func() { code = run([]string{"usage", "--scope", "proxy", "--by", "model"}) })
	if code != 0 || !strings.Contains(stdout, "proxy") || !strings.HasSuffix(strings.TrimSpace(stdout), "-") {
		t.Fatalf("code=%d stdout=%q, want a proxy row ending in cost '-'", code, stdout)
	}
}

// TestRunUsageRejectsBadScope guards --scope's allowed values and the
// daemon's scope/groupBy validation.
func TestRunUsageRejectsBadScope(t *testing.T) {
	newTestProfileDaemon(t)
	for _, args := range [][]string{
		{"usage", "--scope", "bogus"},
		{"usage", "--scope", "runs", "--by", "account"},
	} {
		var code int
		captureStderr(t, func() { code = run(args) })
		if code == 0 {
			t.Errorf("run(%v) = 0, want non-zero", args)
		}
	}
}
