package store

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func openPolicyAuditStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAudit_AppendAndList(t *testing.T) {
	s := openPolicyAuditStore(t)
	paths := make([]string, 12)
	for i := range paths {
		paths[i] = "/w/p" + strconv.Itoa(i) + ".txt"
	}
	createdAt := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	id, err := s.AppendPolicyAudit(PolicyAuditRow{
		CreatedAt:   createdAt,
		RunID:       "run-1",
		TaskID:      7,
		RequestID:   "req-1",
		Seat:        "lead",
		WorkspaceID: 3,
		Tool:        "Bash",
		Target:      strings.Repeat("x", 600),
		Paths:       paths,
		Decision:    "ask",
		RuleID:      "lead-tests",
		PolicySha:   "abc123",
		Reasons:     strings.Repeat("r", 2000),
		LatencyMs:   12,
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListPolicyAudit(PolicyAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.ID != id || got.RunID != "run-1" || got.TaskID != 7 || got.RequestID != "req-1" || got.Seat != "lead" ||
		got.WorkspaceID != 3 || got.Tool != "Bash" || got.Decision != "ask" || got.RuleID != "lead-tests" ||
		got.PolicySha != "abc123" || got.LatencyMs != 12 {
		t.Fatalf("row = %+v", got)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, createdAt)
	}
	if len(got.Target) != policyAuditMaxTarget {
		t.Fatalf("len(Target) = %d, want %d", len(got.Target), policyAuditMaxTarget)
	}
	if len(got.Reasons) != policyAuditMaxReasons {
		t.Fatalf("len(Reasons) = %d, want %d", len(got.Reasons), policyAuditMaxReasons)
	}
	if len(got.Paths) != policyAuditMaxPaths {
		t.Fatalf("len(Paths) = %d, want %d", len(got.Paths), policyAuditMaxPaths)
	}
	for i, p := range got.Paths {
		if want := "/w/p" + strconv.Itoa(i) + ".txt"; p != want {
			t.Fatalf("Paths[%d] = %q, want the first %d paths in order", i, p, policyAuditMaxPaths)
		}
	}

	// A zero CreatedAt is stamped now UTC, not left to the driver.
	before := time.Now().UTC()
	id2, err := s.AppendPolicyAudit(PolicyAuditRow{Tool: "Read", Decision: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := s.ListPolicyAudit(PolicyAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stamped) != 2 || stamped[0].ID != id2 || stamped[1].ID != id {
		t.Fatalf("rows = %+v, want [%d %d] newest first", stamped, id2, id)
	}
	if stamped[0].CreatedAt.Before(before) || stamped[0].CreatedAt.After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("CreatedAt = %v, want ~now", stamped[0].CreatedAt)
	}
}

func TestAudit_RejectsUnknownDecision(t *testing.T) {
	s := openPolicyAuditStore(t)
	for _, d := range []string{"", "maybe", "ALLOW", "policy_allow"} {
		if _, err := s.AppendPolicyAudit(PolicyAuditRow{Decision: d}); err == nil {
			t.Fatalf("decision %q accepted, want error", d)
		}
	}
	for _, d := range []string{"allow", "deny", "ask", "ask_human_allow", "ask_human_deny", "ask_timeout_deny"} {
		if _, err := s.AppendPolicyAudit(PolicyAuditRow{Decision: d}); err != nil {
			t.Fatalf("decision %q rejected: %v", d, err)
		}
	}
	if rows, err := s.ListPolicyAudit(PolicyAuditFilter{}); err != nil || len(rows) != 6 {
		t.Fatalf("rows = %d, %v; want the 6 valid appends", len(rows), err)
	}
}

func TestAudit_ListFilterAndPaging(t *testing.T) {
	s := openPolicyAuditStore(t)
	add := func(taskID int64, decision string) int64 {
		t.Helper()
		id, err := s.AppendPolicyAudit(PolicyAuditRow{TaskID: taskID, Decision: decision})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a1 := add(7, "ask")
	a2 := add(7, "allow")
	a3 := add(7, "ask")
	add(8, "deny")

	rows, err := s.ListPolicyAudit(PolicyAuditFilter{TaskID: 7, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != a3 || rows[1].ID != a2 {
		t.Fatalf("task 7 page 1 = %+v, want [%d %d] newest first", rows, a3, a2)
	}
	rows, err = s.ListPolicyAudit(PolicyAuditFilter{TaskID: 7, BeforeID: a2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != a1 {
		t.Fatalf("task 7 page 2 = %+v, want [%d]", rows, a1)
	}
	rows, err = s.ListPolicyAudit(PolicyAuditFilter{Decision: "deny"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].TaskID != 8 {
		t.Fatalf("deny filter = %+v", rows)
	}
	if rows, err = s.ListPolicyAudit(PolicyAuditFilter{Limit: 501}); err != nil || len(rows) != 4 {
		t.Fatalf("over-cap limit = %d rows, %v; want capped, all 4", len(rows), err)
	}
}

func TestAudit_RetentionSweep90Days(t *testing.T) {
	s := openPolicyAuditStore(t)
	now := time.Now().UTC()
	old := PolicyAuditRow{Decision: "ask", CreatedAt: now.Add(-91 * 24 * time.Hour)}
	if _, err := s.AppendPolicyAudit(old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendPolicyAudit(PolicyAuditRow{Decision: "ask", CreatedAt: now.Add(-89 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendPolicyAudit(PolicyAuditRow{Decision: "allow"}); err != nil {
		t.Fatal(err)
	}

	n, err := s.SweepPolicyAudit(now.Add(-PolicyAuditRetention))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("sweep deleted %d rows, want exactly the 91-day-old one", n)
	}
	rows, err := s.ListPolicyAudit(PolicyAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows after sweep = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.CreatedAt.Before(now.Add(-PolicyAuditRetention)) {
			t.Fatalf("row %d created %v survived the sweep", r.ID, r.CreatedAt)
		}
	}
}

func TestAudit_CapKeepsValidUTF8(t *testing.T) {
	s := openPolicyAuditStore(t)
	if _, err := s.AppendPolicyAudit(PolicyAuditRow{
		Decision: "ask",
		Target:   strings.Repeat("京", 600),
		Reasons:  strings.Repeat("説", 600),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListPolicyAudit(PolicyAuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if got := rows[0].Target; !utf8.ValidString(got) || len(got) > policyAuditMaxTarget {
		t.Fatalf("Target capped to %d bytes of %t valid UTF-8", len(got), utf8.ValidString(got))
	}
	if got := rows[0].Reasons; !utf8.ValidString(got) || len(got) > policyAuditMaxReasons {
		t.Fatalf("Reasons capped to %d bytes of %t valid UTF-8", len(got), utf8.ValidString(got))
	}
}
