package store

import (
	"errors"
	"fmt"
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

func TestAutomationLinksForTasks(t *testing.T) {
	st, ws := openAutomationStore(t)
	a, _ := st.InsertAutomation(Automation{Name: "nightly-build", WorkspaceID: ws.ID, SignalKind: "cron"})
	mkTask := func(title string) int64 {
		task, err := st.CreateTask(Task{WorkspaceID: ws.ID, Title: title, Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	fired, denied, untouched := mkTask("fired"), mkTask("denied"), mkTask("untouched")
	link := func(taskID int64, decision string) AutomationAudit {
		r, err := st.InsertAutomationAudit(AutomationAudit{AutomationID: a.ID, SignalKind: "cron", Fingerprint: fmt.Sprint(taskID), Decision: decision})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetAutomationAuditTask(r.ID, taskID, "run"); err != nil {
			t.Fatal(err)
		}
		return r
	}
	link(fired, AuditFire)
	link(denied, AuditDeduped)

	got, err := st.AutomationLinksForTasks([]int64{fired, denied, untouched})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("links = %+v, want only the fired task", got)
	}
	if l, ok := got[fired]; !ok || l.AutomationID != a.ID || l.Name != "nightly-build" {
		t.Fatalf("link for task %d = %+v ok=%v", fired, l, ok)
	}
	if _, ok := got[denied]; ok {
		t.Fatal("a non-fire decision produced a link")
	}
	if _, ok := got[untouched]; ok {
		t.Fatal("an unaudited task produced a link")
	}

	// A later fire row on the same task wins.
	b, _ := st.InsertAutomation(Automation{Name: "ci-break", WorkspaceID: ws.ID, SignalKind: "github.workflow_run"})
	r, err := st.InsertAutomationAudit(AutomationAudit{AutomationID: b.ID, SignalKind: "github.workflow_run", Fingerprint: "late", Decision: AuditFire})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAutomationAuditTask(r.ID, fired, "run-late"); err != nil {
		t.Fatal(err)
	}
	got, err = st.AutomationLinksForTasks([]int64{fired})
	if err != nil {
		t.Fatal(err)
	}
	if l := got[fired]; l.AutomationID != b.ID || l.Name != "ci-break" {
		t.Fatalf("latest fire row must win: %+v", l)
	}

	if got, err = st.AutomationLinksForTasks(nil); err != nil || len(got) != 0 {
		t.Fatalf("no ids = %+v, %v; want an empty map", got, err)
	}
	ids := make([]int64, 0, 600)
	ids = append(ids, fired)
	for i := int64(0); i < 599; i++ {
		task, err := st.CreateTask(Task{WorkspaceID: ws.ID, Title: "filler", Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	got, err = st.AutomationLinksForTasks(ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[fired].Name != "ci-break" {
		t.Fatalf("600 ids = %+v, want only task %d", got, fired)
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
