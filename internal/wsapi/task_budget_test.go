package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/workspace"
)

func budgetCall(t *testing.T, api *API, rc *requestContext, method string, params any) (json.RawMessage, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	res, err := api.hs[method](context.Background(), rc, raw)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return out, nil
}

// spendOn records one finished user run with a reported cost on taskID.
func spendOn(t *testing.T, db *store.Store, taskID int64, runID string, cost *float64) {
	t.Helper()
	if _, err := db.CreateRun(store.Run{ID: runID, TaskID: taskID, Provider: "glm", Prompt: "p", Status: "done", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if cost != nil {
		if err := db.UpsertRunUsage(store.RunUsage{RunID: runID, TaskID: taskID, Provider: "glm", CostUSD: cost, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
}

func fptr(v float64) *float64 { return &v }

func TestSetBudget_RefusesRunScoped(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	params := map[string]any{"taskId": e.seatTask.ID, "budgetUsd": 5}
	_, err := budgetCall(t, e.api, &requestContext{runScoped: true}, "task.setBudget", params)
	if err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("run-scoped task.setBudget error = %v, want forbidden", err)
	}
	// An identified connection is refused before the handler (not allowlisted).
	resp := callWS(t, e.identified(t), "task.setBudget", params)
	wantForbidden(t, resp, "not available to agents")
	if got, _ := e.db.GetTask(e.seatTask.ID); got.BudgetUSD != nil {
		t.Fatalf("budget = %v after refused calls, want nil", *got.BudgetUSD)
	}
	// A human sets it.
	if _, err := budgetCall(t, e.api, &requestContext{}, "task.setBudget", params); err != nil {
		t.Fatalf("human task.setBudget error = %v", err)
	}
	if got, _ := e.db.GetTask(e.seatTask.ID); got.BudgetUSD == nil || *got.BudgetUSD != 5 {
		t.Fatalf("budget = %v, want 5", got.BudgetUSD)
	}
}

func TestSetBudget_NonRootRefused(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "")
	child, err := wm.CreateTask(root.WorkspaceID, nil, "child", workspace.WithParentTask(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	api, _ := newGitTestAPI(t, wm, db, nil)
	_, err = budgetCall(t, api, &requestContext{}, "task.setBudget", map[string]any{"taskId": child.ID, "budgetUsd": 5})
	if err == nil || codedError(err).Code != codeInvalid {
		t.Fatalf("setBudget on a child error = %v, want invalid_params", err)
	}
	if got, _ := db.GetTask(child.ID); got.BudgetUSD != nil {
		t.Fatal("a child task got a budget")
	}
	// task.create with a budget under a parent is refused the same way.
	_, err = budgetCall(t, api, &requestContext{}, "task.create", map[string]any{"workspaceId": root.WorkspaceID, "title": "x", "parentTaskId": root.ID, "budgetUsd": 5})
	if err == nil || codedError(err).Code != codeInvalid {
		t.Fatalf("task.create with parent + budget error = %v, want invalid_params", err)
	}
}

func TestSetBudget_ValidatesAmount(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "")
	api, _ := newGitTestAPI(t, wm, db, nil)
	set := func(raw string) (json.RawMessage, error) {
		res, err := api.hs["task.setBudget"](context.Background(), &requestContext{}, json.RawMessage(`{"taskId":`+itoa(root.ID)+raw+`}`))
		if err != nil {
			return nil, err
		}
		out, _ := json.Marshal(res)
		return out, nil
	}
	for _, bad := range []string{``, `,"budgetUsd":0`, `,"budgetUsd":-3`, `,"budgetUsd":0.004`, `,"budgetUsd":"5"`, `,"budgetUsd":1e999`, `,"budgetUsd":true`} {
		if _, err := set(bad); err == nil || codedError(err).Code != codeInvalid {
			t.Errorf("budget %q error = %v, want invalid_params", bad, err)
		}
	}
	if got, _ := db.GetTask(root.ID); got.BudgetUSD != nil {
		t.Fatal("a rejected amount was stored")
	}
	out, err := set(`,"budgetUsd":4.126`)
	if err != nil {
		t.Fatal(err)
	}
	var task struct {
		BudgetUSD      *float64 `json:"budgetUsd"`
		BudgetSpentUSD *float64 `json:"budgetSpentUsd"`
	}
	if err := json.Unmarshal(out, &task); err != nil {
		t.Fatal(err)
	}
	if task.BudgetUSD == nil || *task.BudgetUSD != 4.13 || task.BudgetSpentUSD == nil || *task.BudgetSpentUSD != 0 {
		t.Fatalf("result = %s, want budgetUsd 4.13 (rounded) and budgetSpentUsd 0", out)
	}
	if _, err := set(`,"budgetUsd":null`); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetTask(root.ID); got.BudgetUSD != nil {
		t.Fatalf("budget = %v after null, want cleared", *got.BudgetUSD)
	}
}

func TestWSAPI_BudgetExceededCode(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	spendOn(t, e.db, e.seatTask.ID, "spent", fptr(7))
	if err := e.db.SetTaskBudget(e.seatTask.ID, fptr(5)); err != nil {
		t.Fatal(err)
	}

	// run.start with source=agent is refused with the wire code and sentence.
	resp := callWS(t, e.admin, "run.start", map[string]any{
		"taskId": e.seatTask.ID, "provider": "glm", "prompt": "more", "whenBusy": "queue",
		"source": "agent", "fromTaskId": e.seatTask.ID, "fromChatId": e.chatID,
	})
	if resp.Error == nil || resp.Error.Code != "budget_exceeded" {
		t.Fatalf("agent run.start error = %+v, want code budget_exceeded", resp.Error)
	}
	if want := "reached its budget ($7.00 of $5.00 reported cost"; !strings.Contains(resp.Error.Message, want) {
		t.Fatalf("message %q lacks %q", resp.Error.Message, want)
	}

	// A seat's task.create under the over-budget tree is refused too.
	resp = callWS(t, e.identified(t), "task.create", map[string]any{"workspaceId": e.seatTask.WorkspaceID, "title": "child"})
	if resp.Error == nil || resp.Error.Code != "budget_exceeded" {
		t.Fatalf("seat task.create error = %+v, want code budget_exceeded", resp.Error)
	}

	// Humans are never refused: same tree, human source.
	resp = callWS(t, e.admin, "task.create", map[string]any{"workspaceId": e.seatTask.WorkspaceID, "title": "human child", "parentTaskId": e.seatTask.ID})
	wantOK(t, resp, "human task.create")
}

func TestTaskTree_RootBudgetFields(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	root := newTestTask(t, wm, "")
	child, err := wm.CreateTask(root.WorkspaceID, nil, "child", workspace.WithParentTask(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := wm.CreateTask(root.WorkspaceID, nil, "unbudgeted root")
	if err != nil {
		t.Fatal(err)
	}
	spendOn(t, db, root.ID, "a", fptr(2.5))
	spendOn(t, db, child.ID, "b", fptr(1.5))
	spendOn(t, db, child.ID, "c", nil) // reported no cost
	if err := db.SetTaskBudget(root.ID, fptr(4)); err != nil {
		t.Fatal(err)
	}
	api, _ := newGitTestAPI(t, wm, db, nil)

	out, err := budgetCall(t, api, &requestContext{}, "task.tree", map[string]any{"workspaceId": root.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	var tree struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(out, &tree); err != nil {
		t.Fatal(err)
	}
	byID := map[int64]map[string]any{}
	for _, n := range tree.Nodes {
		byID[int64(n["id"].(float64))] = n
	}
	r := byID[root.ID]
	if r["budgetUsd"] != 4.0 || r["budgetSpentUsd"] != 4.0 || r["budgetPartial"] != true || r["budgetUnknownRuns"] != 1.0 || r["overBudget"] != true {
		t.Fatalf("root node = %v, want budgetUsd 4, spent 4, partial, 1 unknown, over", r)
	}
	for _, id := range []int64{child.ID, plain.ID} {
		for _, k := range []string{"budgetUsd", "budgetSpentUsd", "budgetPartial", "budgetUnknownRuns", "overBudget"} {
			if _, ok := byID[id][k]; ok {
				t.Errorf("node %d carries %s, want it omitted (no budget)", id, k)
			}
		}
	}

	// The task payload carries the same fields on the root and not elsewhere.
	out, err = budgetCall(t, api, &requestContext{}, "task.get", map[string]any{"id": root.ID})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["budgetUsd"] != 4.0 || got["budgetSpentUsd"] != 4.0 || got["overBudget"] != true || got["budgetPartial"] != true {
		t.Fatalf("task.get = %v, want the budget fields", got)
	}
	out, _ = budgetCall(t, api, &requestContext{}, "task.get", map[string]any{"id": child.ID})
	got = nil
	_ = json.Unmarshal(out, &got)
	if _, ok := got["budgetSpentUsd"]; ok {
		t.Fatalf("child task.get = %v, want no budget fields", got)
	}
}

func TestTaskCreate_BudgetHumanRootOnly(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	seed := newTestTask(t, wm, "")
	api, _ := newGitTestAPI(t, wm, db, nil)
	params := map[string]any{"workspaceId": seed.WorkspaceID, "title": "budgeted", "budgetUsd": 3.5}

	out, err := budgetCall(t, api, &requestContext{}, "task.create", params)
	if err != nil {
		t.Fatalf("human task.create with a budget error = %v", err)
	}
	var created struct {
		ID        int64    `json:"ID"`
		BudgetUSD *float64 `json:"budgetUsd"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		t.Fatal(err)
	}
	if created.BudgetUSD == nil || *created.BudgetUSD != 3.5 {
		t.Fatalf("created = %s, want budgetUsd 3.5", out)
	}
	if got, _ := db.GetTask(created.ID); got.BudgetUSD == nil || *got.BudgetUSD != 3.5 {
		t.Fatalf("stored budget = %v, want 3.5", got.BudgetUSD)
	}
	if _, err := budgetCall(t, api, &requestContext{runScoped: true, ident: runIdentity{taskID: seed.ID}}, "task.create", params); err == nil || codedError(err).Code != codeForbidden {
		t.Fatalf("run-scoped task.create with a budget error = %v, want forbidden", err)
	}
}
