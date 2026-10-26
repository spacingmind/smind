package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openAutomationStore(t *testing.T) (*Store, Workspace) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ws, err := st.CreateWorkspace(Workspace{Path: "/tmp/w", Title: "w"})
	if err != nil {
		t.Fatal(err)
	}
	return st, ws
}

func TestMigrate_AutomationTables(t *testing.T) {
	st, ws := openAutomationStore(t)
	a, err := st.InsertAutomation(Automation{Name: "a", WorkspaceID: ws.ID, SignalKind: "cron", Enabled: true, Status: AutomationPaused})
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the caller says, a new automation is disabled and active.
	if a.Enabled || a.Status != AutomationActive || a.Signal != "{}" {
		t.Fatalf("inserted = %+v", a)
	}
	if _, err := st.InsertAutomation(Automation{Name: "a", WorkspaceID: ws.ID, SignalKind: "cron"}); !errors.Is(err, ErrAutomationExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := st.GetAutomation(999); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestAutomationAudit_Queries(t *testing.T) {
	st, ws := openAutomationStore(t)
	a, _ := st.InsertAutomation(Automation{Name: "a", WorkspaceID: ws.ID, SignalKind: "cron"})
	now := time.Now().UTC()
	add := func(decision, fp string, at time.Time) AutomationAudit {
		r, err := st.InsertAutomationAudit(AutomationAudit{AutomationID: a.ID, SignalKind: "cron", Fingerprint: fp, Decision: decision, CreatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	f1 := add(AuditFire, "x", now.Add(-30*time.Minute))
	add(AuditDeduped, "x", now.Add(-10*time.Minute))
	add(AuditFire, "y", now.Add(-2*time.Hour))
	if n, _ := st.CountAutomationFiresSince(a.ID, now.Add(-time.Hour)); n != 1 {
		t.Fatalf("fires in the last hour = %d", n)
	}
	// A deduped row does not extend the window: only the fire row counts.
	if ok, _ := st.HasAutomationFireSince(a.ID, "x", now.Add(-20*time.Minute)); ok {
		t.Fatal("a deduped row counted as a fire")
	}
	if ok, _ := st.HasAutomationFireSince(a.ID, "x", now.Add(-time.Hour)); !ok {
		t.Fatal("fire row not found")
	}
	if err := st.SetAutomationAuditTask(f1.ID, 7, "run-7"); err != nil {
		t.Fatal(err)
	}
	if r, ok, _ := st.AutomationAuditForTask(7); !ok || r.ID != f1.ID {
		t.Fatalf("task link = %+v %v", r, ok)
	}
	open, _ := st.OpenAutomationFirings(a.ID)
	if len(open) != 2 {
		t.Fatalf("open firings = %d", len(open))
	}
	if err := st.FinishAutomationAudit(f1.ID, "done", "", now); err != nil {
		t.Fatal(err)
	}
	if open, _ = st.OpenAutomationFirings(a.ID); len(open) != 1 {
		t.Fatalf("open firings after finish = %d", len(open))
	}
	rows, _ := st.ListAutomationAudit(a.ID, 2, 0)
	if len(rows) != 2 || rows[0].ID < rows[1].ID {
		t.Fatalf("not newest first: %+v", rows)
	}
	older, _ := st.ListAutomationAudit(a.ID, 10, rows[1].ID)
	if len(older) != 1 {
		t.Fatalf("paging = %d", len(older))
	}
}

func TestAutomation_DeleteCascadesAuditAndWorkspaceDeleteDoesNotBreak(t *testing.T) {
	st, ws := openAutomationStore(t)
	a, _ := st.InsertAutomation(Automation{Name: "a", WorkspaceID: ws.ID, SignalKind: "cron"})
	_, _ = st.InsertAutomationAudit(AutomationAudit{AutomationID: a.ID, SignalKind: "cron", Fingerprint: "f", Decision: AuditPaused})
	if err := st.DeleteAutomation(a.ID); err != nil {
		t.Fatal(err)
	}
	if rows, _ := st.ListAutomationAudit(a.ID, 10, 0); len(rows) != 0 {
		t.Fatal("audit rows survived the automation")
	}
}

func TestSetAutomationEnabled_ReprimesScheduleAndCursor(t *testing.T) {
	st, ws := openAutomationStore(t)
	a, _ := st.InsertAutomation(Automation{Name: "a", WorkspaceID: ws.ID, SignalKind: "github.workflow_run"})
	next := time.Now().Add(time.Hour)
	_ = st.SetAutomationPoll(a.ID, "123", &next, 2)
	_ = st.SetAutomationNextRun(a.ID, &next)
	if err := st.SetAutomationEnabled(a.ID, true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetAutomation(a.ID)
	if got.PollCursor != "" || got.NextRunAt != nil || got.PollFailures != 0 || !got.Enabled {
		t.Fatalf("enable did not re-prime: %+v", got)
	}
}
