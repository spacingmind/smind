package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// seedArchivedRun persists a finished run row for taskID that the Registry
// does not hold, like the run of an old archived task.
func seedArchivedRun(t *testing.T, db *store.Store, id string, taskID, profileID int64) {
	t.Helper()
	started := time.Now().UTC().Add(-time.Hour)
	fin := started.Add(time.Minute)
	if _, err := db.CreateRun(store.Run{
		ID: id, TaskID: taskID, Provider: "glm", Prompt: "old", Status: "done",
		StartedAt: started, FinishedAt: &fin, ProfileID: profileID,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunList_ArchivedTaskCarriesProfileAndParent(t *testing.T) {
	t.Parallel()
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		return e.mkProfile(t, seatProfile("lead", "none"))
	})
	parent := e.taskUnder(t, e.seatTask.ID, false)
	child := e.taskUnder(t, parent.ID, false)
	named := e.peer(t, "peer-named")
	gone := e.peer(t, "peer-gone")
	seedArchivedRun(t, e.db, "r-named", child.ID, named.ID)
	seedArchivedRun(t, e.db, "r-gone", child.ID, gone.ID)
	if err := e.db.DeleteAgentProfile(gone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.wm.ArchiveTask(child.ID); err != nil {
		t.Fatal(err)
	}

	resp := callWS(t, e.admin, "run.list", map[string]any{"taskId": child.ID})
	wantOK(t, resp, "run.list")
	var rows []struct {
		ID           string
		ProfileID    int64
		ProfileName  *string `json:"profileName"`
		ParentTaskID *int64  `json:"parentTaskId"`
	}
	if err := json.Unmarshal(resp.Result, &rows); err != nil {
		t.Fatal(err)
	}
	byID := map[string]int{}
	for i, r := range rows {
		byID[r.ID] = i
		if r.ParentTaskID == nil || *r.ParentTaskID != parent.ID {
			t.Errorf("run %s parentTaskId = %v, want %d", r.ID, r.ParentTaskID, parent.ID)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the 2 persisted runs of the archived task", rows)
	}
	if n := rows[byID["r-named"]].ProfileName; n == nil || *n != "peer-named" {
		t.Errorf("r-named profileName = %v, want peer-named", n)
	}
	if g := rows[byID["r-gone"]]; g.ProfileName != nil || g.ProfileID != gone.ID {
		t.Errorf("r-gone = %+v, want profileId kept and profileName null (profile deleted)", g)
	}

	logs := callWS(t, e.admin, "run.logs", map[string]any{"runId": "r-named"})
	wantOK(t, logs, "run.logs of a persisted run")
}

func TestTaskList_WithLatestRunProfile(t *testing.T) {
	t.Parallel()
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		return e.mkProfile(t, seatProfile("lead", "none"))
	})
	task := e.taskUnder(t, e.seatTask.ID, false)
	p := e.peer(t, "peer-x")
	seedArchivedRun(t, e.db, "r-1", task.ID, p.ID)
	if _, err := e.wm.ArchiveTask(task.ID); err != nil {
		t.Fatal(err)
	}
	resp := callWS(t, e.admin, "task.list", map[string]any{"workspaceId": task.WorkspaceID, "archived": true, "withLatestRun": true})
	wantOK(t, resp, "task.list")
	var rows []struct {
		ID                   int64
		LatestRunProfileID   *int64  `json:"latestRunProfileId"`
		LatestRunProfileName *string `json:"latestRunProfileName"`
	}
	if err := json.Unmarshal(resp.Result, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("rows = %s (%v)", resp.Result, err)
	}
	if rows[0].LatestRunProfileID == nil || *rows[0].LatestRunProfileID != p.ID || rows[0].LatestRunProfileName == nil || *rows[0].LatestRunProfileName != "peer-x" {
		t.Fatalf("row = %+v, want latest run profile peer-x", rows[0])
	}
}

// An agent may read archived history inside its own subtree only.
func TestAdmission_ArchivedReadsStayInOwnSubtree(t *testing.T) {
	t.Parallel()
	e := newAdmEnv(t, func(e *admEnv) store.AgentProfile {
		return e.mkProfile(t, seatProfile("lead", "none"))
	})
	ws := e.identified(t)

	own := e.taskUnder(t, e.seatTask.ID, false)
	sibling := e.sibling(t)
	foreign := e.taskUnder(t, sibling.ID, false)
	seedArchivedRun(t, e.db, "r-own", own.ID, 0)
	seedArchivedRun(t, e.db, "r-foreign", foreign.ID, 0)
	for _, id := range []int64{own.ID, foreign.ID} {
		if _, err := e.wm.ArchiveTask(id); err != nil {
			t.Fatal(err)
		}
	}
	wsID := e.seatTask.WorkspaceID

	// Own subtree: allowed.
	wantOK(t, callWS(t, ws, "task.list", map[string]any{"workspaceId": wsID, "archived": true, "parentTaskId": e.seatTask.ID}), "own archived task.list")
	wantOK(t, callWS(t, ws, "task.tree", map[string]any{"taskId": own.ID}), "own archived task.tree")
	wantOK(t, callWS(t, ws, "task.tree", map[string]any{"taskId": e.seatTask.ID, "includeArchived": true}), "own includeArchived task.tree")
	wantOK(t, callWS(t, ws, "run.list", map[string]any{"taskId": own.ID}), "own archived run.list")
	wantOK(t, callWS(t, ws, "run.logs", map[string]any{"runId": "r-own"}), "own archived run.logs")

	// Outside it: refused.
	const scope = "outside your own task subtree"
	wantForbidden(t, callWS(t, ws, "task.list", map[string]any{"workspaceId": wsID, "archived": true}), scope)
	wantForbidden(t, callWS(t, ws, "task.list", map[string]any{"workspaceId": wsID, "archived": true, "parentTaskId": sibling.ID}), scope)
	wantForbidden(t, callWS(t, ws, "task.tree", map[string]any{"taskId": foreign.ID}), scope)
	wantForbidden(t, callWS(t, ws, "task.tree", map[string]any{"workspaceId": wsID, "includeArchived": true}), scope)
	wantForbidden(t, callWS(t, ws, "task.tree", map[string]any{"taskId": sibling.ID, "includeArchived": true}), scope)
	wantForbidden(t, callWS(t, ws, "run.list", map[string]any{"taskId": foreign.ID}), scope)
	wantForbidden(t, callWS(t, ws, "run.logs", map[string]any{"runId": "r-foreign"}), scope)

	// A human (unidentified) connection reads them.
	wantOK(t, callWS(t, e.admin, "run.logs", map[string]any{"runId": "r-foreign"}), "human archived run.logs")
}
