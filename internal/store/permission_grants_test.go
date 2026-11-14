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

func grantWorkspaces(t *testing.T, s *Store, n int) []int64 {
	t.Helper()
	ids := make([]int64, n)
	for i := range ids {
		w, err := s.CreateWorkspace(Workspace{Path: t.TempDir(), Title: "W"})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = w.ID
	}
	return ids
}

func TestGrants_CreateListRevoke(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := grantWorkspaces(t, s, 2)
	g, err := s.SaveGrant(sampleGrant(ws[0], "aa"))
	if err != nil || g.ID == 0 {
		t.Fatalf("SaveGrant = %+v, %v", g, err)
	}
	got, ok, err := s.FindGrant(ws[0], "aa")
	if err != nil || !ok || got.CommandDisplay != "make build" || got.MCPFingerprint != "fp1" || got.RevokedAt != nil {
		t.Fatalf("FindGrant = %+v %v %v", got, ok, err)
	}
	if _, ok, _ := s.FindGrant(ws[1], "aa"); ok {
		t.Fatal("another project found the grant")
	}
	if _, ok, _ := s.FindGrant(ws[0], "bb"); ok {
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
	if l, _ := s.ListGrants(ws[1]); len(l) != 0 {
		t.Fatalf("project filter returned %d", len(l))
	}
}

func TestGrants_UniquePerProjectAndCommand(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := grantWorkspaces(t, s, 2)
	first, _ := s.SaveGrant(sampleGrant(ws[0], "aa"))
	_, _ = s.RevokeGrant(first.ID, "local", time.Now())
	again, err := s.SaveGrant(sampleGrant(ws[0], "aa"))
	if err != nil || again.ID != first.ID || again.RevokedAt != nil || again.UseCount != 0 {
		t.Fatalf("re-save = %+v, %v (want the same row, revival cleared)", again, err)
	}
	if list, _ := s.ListGrants(0); len(list) != 1 {
		t.Fatalf("%d rows for one (project, command)", len(list))
	}
	other, _ := s.SaveGrant(sampleGrant(ws[1], "aa"))
	if other.ID == first.ID {
		t.Fatal("two projects share a row")
	}
}

func TestGrants_AuditRows(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	g, _ := s.SaveGrant(sampleGrant(grantWorkspaces(t, s, 1)[0], "aa"))
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

// A revoke that lands between a caller's check and the use still wins, and an
// expired grant is not usable: the update itself is conditional.
func TestGrants_ExpireAndVoidNotApplied(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	g, _ := s.SaveGrant(sampleGrant(grantWorkspaces(t, s, 1)[0], "aa"))
	now := g.CreatedAt.Add(time.Hour)
	if err := s.RecordGrantUse(g.ID, "r", "q", now); err != nil {
		t.Fatalf("an active grant refused a use: %v", err)
	}
	if err := s.RecordGrantUse(g.ID, "r", "q", g.ExpiresAt); err != ErrGrantInactive {
		t.Fatalf("a use at expiry = %v, want ErrGrantInactive", err)
	}
	_, _ = s.RevokeGrant(g.ID, "local", now)
	if err := s.RecordGrantUse(g.ID, "r", "q", now); err != ErrGrantInactive {
		t.Fatalf("a use after revoke = %v, want ErrGrantInactive", err)
	}
	if err := s.RecordGrantUse(9999, "r", "q", now); err != ErrGrantInactive {
		t.Fatalf("a use of a missing grant = %v", err)
	}
	if got, _ := s.GetGrant(g.ID); got.UseCount != 1 {
		t.Fatalf("use count = %d, want only the first use", got.UseCount)
	}
	rows, _ := s.ListGrantAudit(g.ID)
	used := 0
	for _, r := range rows {
		if r.Kind == GrantAuditUsed {
			used++
		}
	}
	if used != 1 {
		t.Fatalf("%d used rows for one use", used)
	}
}

func TestGrants_SkipIsAuditedOncePerEpisode(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	g, _ := s.SaveGrant(sampleGrant(grantWorkspaces(t, s, 1)[0], "aa"))
	for i := 0; i < 5; i++ {
		_ = s.NoteGrantSkip(GrantAuditRow{GrantID: g.ID, At: time.Now(), Kind: GrantAuditSkippedExpired, Actor: "daemon"})
	}
	_ = s.NoteGrantSkip(GrantAuditRow{GrantID: g.ID, At: time.Now(), Kind: GrantAuditSkippedVoid, Actor: "daemon"})
	rows, _ := s.ListGrantAudit(g.ID)
	if len(rows) != 3 { // created, one expired skip, one void skip
		t.Fatalf("audit rows = %d, want 3: %+v", len(rows), rows)
	}
}

func TestGrants_WorkspaceDeleteRemovesItsGrants(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := grantWorkspaces(t, s, 1)
	g, _ := s.SaveGrant(sampleGrant(ws[0], "aa"))
	if _, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, ws[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetGrant(g.ID); err != ErrGrantNotFound {
		t.Fatalf("grant after its workspace was deleted = %v", err)
	}
	if rows, _ := s.ListGrantAudit(g.ID); len(rows) != 0 {
		t.Fatalf("audit rows left: %+v", rows)
	}
}
