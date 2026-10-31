//go:build !windows

package wsapi

import (
	"encoding/json"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

// task.list and task.get expose automationId/automationName on a task an
// automation's fire row started, and omit both keys on every other task.
func TestTaskListExposesAutomationLink(t *testing.T) {
	t.Parallel()
	e := newPREnv(t)
	other, err := e.wm.CreateTask(e.task.WorkspaceID, nil, "other task")
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.db.InsertAutomation(store.Automation{Name: "nightly build", WorkspaceID: e.task.WorkspaceID, SignalKind: "cron"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.db.InsertAutomationAudit(store.AutomationAudit{AutomationID: a.ID, SignalKind: "cron", Fingerprint: "f", Decision: store.AuditFire})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.SetAutomationAuditTask(r.ID, e.task.ID, "run"); err != nil {
		t.Fatal(err)
	}

	rowByID := func(t *testing.T, res any) map[int64]map[string]any {
		t.Helper()
		b, err := json.Marshal(res)
		if err != nil {
			t.Fatal(err)
		}
		var rows []map[string]any
		if err := json.Unmarshal(b, &rows); err != nil {
			t.Fatal(err)
		}
		out := map[int64]map[string]any{}
		for _, m := range rows {
			out[int64(m["ID"].(float64))] = m
		}
		return out
	}

	list, err := e.call("task.list", map[string]any{"workspaceId": e.task.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	linked, unlinked := rowByID(t, list)[e.task.ID], rowByID(t, list)[other.ID]
	if linked == nil || unlinked == nil {
		t.Fatalf("task.list rows = %v", list)
	}
	if id, _ := linked["automationId"].(float64); int64(id) != a.ID {
		t.Fatalf("automationId = %v, want %d", linked["automationId"], a.ID)
	}
	if linked["automationName"] != a.Name {
		t.Fatalf("automationName = %v, want %q", linked["automationName"], a.Name)
	}
	if _, ok := unlinked["automationId"]; ok {
		t.Fatalf("task %d carries automationId: %v", other.ID, unlinked)
	}
	if _, ok := unlinked["automationName"]; ok {
		t.Fatalf("task %d carries automationName: %v", other.ID, unlinked)
	}

	res, err := e.call("task.get", map[string]any{"id": e.task.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := asMap(t, res)
	if id, _ := got["automationId"].(float64); int64(id) != a.ID {
		t.Fatalf("task.get automationId = %v, want %d", got["automationId"], a.ID)
	}
	if got["automationName"] != a.Name {
		t.Fatalf("task.get automationName = %v, want %q", got["automationName"], a.Name)
	}
}
