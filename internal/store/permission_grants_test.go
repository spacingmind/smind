package store

import (
	"fmt"
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

func TestGrants_CapPerWorkspaceAndAuditRetention(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := grantWorkspaces(t, s, 2)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	mk := func(w int64, i int) PermissionGrant {
		g := sampleGrant(w, fmt.Sprintf("sha-%d", i))
		g.CreatedAt, g.ExpiresAt = now, now.Add(30*24*time.Hour)
		return g
	}
	for i := 0; i < MaxGrantsPerWorkspace; i++ {
		if _, err := s.SaveGrant(mk(ws[0], i)); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	// All live: the next one is refused; another project is unaffected; re-saving an existing one still works.
	if _, err := s.SaveGrant(mk(ws[0], 1000)); err != ErrGrantLimit {
		t.Fatalf("past the cap = %v, want ErrGrantLimit", err)
	}
	if _, err := s.SaveGrant(mk(ws[1], 1000)); err != nil {
		t.Fatalf("another project: %v", err)
	}
	if _, err := s.SaveGrant(mk(ws[0], 5)); err != nil {
		t.Fatalf("re-save of an existing grant: %v", err)
	}
	// A revoked grant makes room: the oldest dead one is dropped.
	first, _, _ := s.FindGrant(ws[0], "sha-0")
	_, _ = s.RevokeGrant(first.ID, "local", now)
	if _, err := s.SaveGrant(mk(ws[0], 1000)); err != nil {
		t.Fatalf("after a revoke: %v", err)
	}
	if _, ok, _ := s.FindGrant(ws[0], "sha-0"); ok {
		t.Fatal("the revoked grant was not pruned")
	}
	if l, _ := s.ListGrants(ws[0]); len(l) != MaxGrantsPerWorkspace {
		t.Fatalf("%d grants, want the cap", len(l))
	}
	// Audit retention: a very busy grant keeps only its latest rows.
	g, _, _ := s.FindGrant(ws[0], "sha-7")
	for i := 0; i < MaxGrantAuditRows+50; i++ {
		if err := s.RecordGrantUse(g.ID, "r", fmt.Sprintf("q%d", i), now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := s.ListGrantAudit(g.ID)
	if len(rows) != MaxGrantAuditRows {
		t.Fatalf("%d audit rows, want %d", len(rows), MaxGrantAuditRows)
	}
	if got, _ := s.GetGrant(g.ID); got.UseCount != MaxGrantAuditRows+50 {
		t.Fatalf("use count %d must keep counting past the retention", got.UseCount)
	}
	if rows[len(rows)-1].RequestID != fmt.Sprintf("q%d", MaxGrantAuditRows+49) {
		t.Fatalf("the newest row was pruned: %+v", rows[len(rows)-1])
	}
}

// The store refuses a runner grant without the opt-in, never keeps an opt-in on a
// read-only grant, and refuses an unknown class.
func TestGrants_RunnerNeedsOptInInStore(t *testing.T) {
	t.Parallel()
	s := newTestStore(t)
	ws := grantWorkspaces(t, s, 1)[0]
	g := sampleGrant(ws, "r1")
	g.Class = "runner"
	if _, err := s.SaveGrant(g); err != ErrRunnerNeedsOptIn {
		t.Fatalf("a runner grant without the opt-in = %v", err)
	}
	g.RunnerOptIn = true
	saved, err := s.SaveGrant(g)
	if err != nil || saved.Class != "runner" || !saved.RunnerOptIn {
		t.Fatalf("an opted-in runner grant = %+v, %v", saved, err)
	}
	ro := sampleGrant(ws, "r2")
	ro.Class, ro.RunnerOptIn = "readonly", true
	if saved, _ := s.SaveGrant(ro); saved.RunnerOptIn {
		t.Fatal("an opt-in was kept on a read-only grant")
	}
	// An unset class means read-only, never runner.
	def := sampleGrant(ws, "r3")
	if saved, _ := s.SaveGrant(def); saved.Class != "readonly" {
		t.Fatalf("default class = %q", saved.Class)
	}
	bad := sampleGrant(ws, "r4")
	bad.Class = "admin"
	if _, err := s.SaveGrant(bad); err == nil {
		t.Fatal("an unknown class was stored")
	}
	// Re-saving a runner grant as read-only (or the reverse) replaces the class: the key is project + command.
	g.Class, g.RunnerOptIn = "readonly", false
	if saved, _ := s.SaveGrant(g); saved.Class != "readonly" || saved.RunnerOptIn {
		t.Fatalf("re-save = %+v", saved)
	}
}
