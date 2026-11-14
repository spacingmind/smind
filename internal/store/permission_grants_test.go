package store

import (
	"testing"
	"time"
)

func sampleGrant(ws int64, sha string) PermissionGrant {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	return PermissionGrant{WorkspaceID: ws, CommandSHA: sha, CommandLen: 10, CommandDisplay: "make build", Folder: "/repo", MCPFingerprint: "fp1",
		CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), CreatedBy: "local", SourceRunID: "run-1", SourceRequestID: "req-1"}
}

func TestGrants_CreateListRevoke(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	g, err := s.SaveGrant(sampleGrant(1, "aa"))
	if err != nil || g.ID == 0 {
		t.Fatalf("SaveGrant = %+v, %v", g, err)
	}
	got, ok, err := s.FindGrant(1, "aa")
	if err != nil || !ok || got.CommandDisplay != "make build" || got.MCPFingerprint != "fp1" || got.RevokedAt != nil {
		t.Fatalf("FindGrant = %+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.FindGrant(2, "aa"); ok {
		t.Fatal("another project found the grant")
	}
	if _, ok, _ := s.FindGrant(1, "bb"); ok {
		t.Fatal("another command found the grant")
	}
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	if changed, err := s.RevokeGrant(g.ID, "local", now); err != nil || !changed {
		t.Fatalf("RevokeGrant = %v, %v", changed, err)
	}
	if changed, err := s.RevokeGrant(g.ID, "local", now); err != nil || changed {
		t.Fatalf("second RevokeGrant = %v, %v (want a quiet no-op)", changed, err)
	}
	if _, err := s.RevokeGrant(999, "local", now); err != ErrGrantNotFound {
		t.Fatalf("RevokeGrant(missing) = %v", err)
	}
	list, _ := s.ListGrants(0)
	if len(list) != 1 || list[0].RevokedAt == nil || list[0].RevokedBy != "local" {
		t.Fatalf("ListGrants = %+v", list)
	}
	if l, _ := s.ListGrants(2); len(l) != 0 {
		t.Fatalf("project filter returned %d", len(l))
	}
}

func TestGrants_UniquePerProjectAndCommand(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	first, _ := s.SaveGrant(sampleGrant(1, "aa"))
	_, _ = s.RevokeGrant(first.ID, "local", time.Now())
	again, err := s.SaveGrant(sampleGrant(1, "aa"))
	if err != nil || again.ID != first.ID || again.RevokedAt != nil || again.UseCount != 0 {
		t.Fatalf("re-save = %+v, %v (want the same row, revival cleared)", again, err)
	}
	if list, _ := s.ListGrants(0); len(list) != 1 {
		t.Fatalf("%d rows for one (project, command)", len(list))
	}
	other, _ := s.SaveGrant(sampleGrant(2, "aa"))
	if other.ID == first.ID {
		t.Fatal("two projects share a row")
	}
}

func TestGrants_AuditRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	g, _ := s.SaveGrant(sampleGrant(1, "aa"))
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	if err := s.RecordGrantUse(g.ID, "run-9", "req-9", now); err != nil {
		t.Fatal(err)
	}
	_ = s.AppendGrantAudit(GrantAuditRow{GrantID: g.ID, At: now, Kind: GrantAuditSkippedExpired, Actor: "daemon", RunID: "run-9", RequestID: "req-10"})
	_, _ = s.RevokeGrant(g.ID, "device:abc", now)
	rows, err := s.ListGrantAudit(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, r := range rows {
		kinds = append(kinds, r.Kind)
	}
	want := []string{GrantAuditCreated, GrantAuditUsed, GrantAuditSkippedExpired, GrantAuditRevoked}
	if len(kinds) != len(want) {
		t.Fatalf("audit kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("audit kinds = %v, want %v", kinds, want)
		}
	}
	if got, _ := s.GetGrant(g.ID); got.UseCount != 1 || got.LastUsedAt == nil {
		t.Fatalf("use not counted: %+v", got)
	}
}
