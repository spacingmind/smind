package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestFormatBudgetLine(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	tests := []struct {
		name string
		v    taskBudgetView
		want string
	}{
		{"none", taskBudgetView{}, "no budget"},
		{"none on a sub-task", taskBudgetView{ParentTaskID: new(int64)}, "no budget (a budget lives on the tree's root task, not on a sub-task)"},
		{"under", taskBudgetView{BudgetUSD: f(5), BudgetSpentUSD: f(1.5)}, "budget $5.00 · spent $1.50 · remaining $3.50"},
		{"partial", taskBudgetView{BudgetUSD: f(5), BudgetSpentUSD: f(4.2), BudgetPartial: true, BudgetUnknownRuns: 3}, "budget $5.00 · spent $4.20+ · partial: 3 runs reported no cost · remaining $0.80"},
		{"partial singular", taskBudgetView{BudgetUSD: f(5), BudgetSpentUSD: f(1), BudgetPartial: true, BudgetUnknownRuns: 1}, "budget $5.00 · spent $1.00+ · partial: 1 run reported no cost · remaining $4.00"},
		{"over", taskBudgetView{BudgetUSD: f(5), BudgetSpentUSD: f(5.02), OverBudget: true}, "budget $5.00 · spent $5.02 · over budget"},
		{"over and partial", taskBudgetView{BudgetUSD: f(5), BudgetSpentUSD: f(5), OverBudget: true, BudgetPartial: true, BudgetUnknownRuns: 2}, "budget $5.00 · spent $5.00+ · partial: 2 runs reported no cost · over budget"},
	}
	for _, tc := range tests {
		if got := formatBudgetLine(tc.v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTaskBudgetCLI_NewFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID := strconv.FormatInt(createCLIHierarchyWorkspace(t, srvURL), 10)

	code, out := runCaptured(t, []string{"task", "new", wsID, "budgeted", "root", "--budget", "7.5"})
	if code != 0 {
		t.Fatalf("task new --budget = %d, out %q", code, out)
	}
	id := firstFieldOfOutputLine(t, out)
	code, out = runCaptured(t, []string{"task", "budget", id})
	if want := "budget $7.50 · spent $0.00 · remaining $7.50\n"; code != 0 || out != want {
		t.Fatalf("task budget = %d, %q, want %q", code, out, want)
	}
	// No flag, no budget.
	code, out = runCaptured(t, []string{"task", "new", wsID, "plain"})
	if code != 0 {
		t.Fatalf("task new = %d", code)
	}
	if _, out := runCaptured(t, []string{"task", "budget", firstFieldOfOutputLine(t, out)}); out != "no budget\n" {
		t.Fatalf("task budget (no flag) = %q, want no budget", out)
	}
	// Bad amounts are usage errors, and nothing is created.
	for _, bad := range []string{"0", "-2", "abc", "NaN", "Inf"} {
		if code, _ := runCaptured(t, []string{"task", "new", wsID, "x", "--budget", bad}); code != 2 {
			t.Errorf("task new --budget %s = %d, want 2", bad, code)
		}
	}
	// A budget on a sub-task is refused by the daemon.
	if code, _ := runCaptured(t, []string{"task", "new", wsID, "child", "--parent", id, "--budget", "1"}); code != 1 {
		t.Errorf("task new --parent --budget = %d, want 1 (daemon refusal)", code)
	}
}

func TestTaskBudgetCLI_ShowSetClear(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	wsID := strconv.FormatInt(createCLIHierarchyWorkspace(t, srvURL), 10)
	_, out := runCaptured(t, []string{"task", "new", wsID, "root"})
	root := firstFieldOfOutputLine(t, out)
	_, out = runCaptured(t, []string{"task", "new", wsID, "kid", "--parent", root})
	kid := firstFieldOfOutputLine(t, out)

	steps := []struct {
		args []string
		code int
		want string
	}{
		{[]string{"task", "budget", root}, 0, "no budget\n"},
		{[]string{"task", "budget", root, "5"}, 0, "budget $5.00 · spent $0.00 · remaining $5.00\n"},
		{[]string{"task", "budget", root}, 0, "budget $5.00 · spent $0.00 · remaining $5.00\n"},
		{[]string{"task", "budget", root, "12.345"}, 0, "budget $12.35 · spent $0.00 · remaining $12.35\n"},
		{[]string{"task", "budget", kid}, 0, "no budget (a budget lives on the tree's root task, not on a sub-task)\n"},
		{[]string{"task", "budget", kid, "3"}, 1, ""},
		{[]string{"task", "budget", root, "--clear"}, 0, "no budget\n"},
		{[]string{"task", "budget", root}, 0, "no budget\n"},
		{[]string{"task", "budget", root, "0"}, 2, ""},
		{[]string{"task", "budget", root, "-1"}, 2, ""},
		{[]string{"task", "budget", root, "5", "--clear"}, 2, ""},
		{[]string{"task", "budget"}, 2, ""},
		{[]string{"task", "budget", "99999"}, 1, ""},
	}
	for _, s := range steps {
		code, out := runCaptured(t, s.args)
		if code != s.code || out != s.want {
			t.Errorf("%v = %d, %q, want %d, %q", s.args, code, out, s.code, s.want)
		}
	}
}

func TestMCP_NoBudgetSetter_ReadOnlyFields(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		if strings.Contains(strings.ToLower(name), "budget") {
			t.Fatalf("tools/list contains budget tool %q: the budget is human-only (ADR-0041 A5)", name)
		}
	}
	// No tool takes a budget argument either (task_new must not accept one).
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		schema, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(strings.ToLower(string(schema)), "budget") {
			t.Fatalf("tool %q input schema mentions a budget: %s", tool.Name, schema)
		}
	}

	// The fields ARE visible, read-only, on task_get and the root of task_tree.
	wsID := createTreeWorkspace(t, env)
	root := createTreeTask(t, env, wsID, 0, "root", "")
	kid := createTreeTask(t, env, wsID, root, "kid", "")
	if err := wsCall(t, env.srvURL, "task.setBudget", map[string]any{"taskId": root, "budgetUsd": 4}, nil); err != nil {
		t.Fatal(err)
	}
	var got taskGetOutput
	if isErr := callMCPTool(t, env.cs, "task_get", map[string]any{"id": root}, &got); isErr {
		t.Fatal("task_get returned an error")
	}
	if got.Task["budgetUsd"] != 4.0 || got.Task["budgetSpentUsd"] != 0.0 || got.Task["budgetRemainingUsd"] != 4.0 || got.Task["overBudget"] != nil && got.Task["overBudget"] != false {
		t.Fatalf("task_get = %v, want budgetUsd 4, spent 0, remaining 4", got.Task)
	}
	var tree taskTreeOutput
	if isErr := callMCPTool(t, env.cs, "task_tree", map[string]any{"workspaceId": wsID}, &tree); isErr {
		t.Fatal("task_tree returned an error")
	}
	rootNode := tree.Roots[0]
	if rootNode["budgetUsd"] != 4.0 || rootNode["budgetRemainingUsd"] != 4.0 || rootNode["budgetSpentUsd"] != 0.0 {
		t.Fatalf("task_tree root = %v, want the budget fields", rootNode)
	}
	children, _ := rootNode["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("children = %v, want the one sub-task", rootNode["children"])
	}
	if node, _ := children[0].(map[string]any); node["id"] != float64(kid) || node["budgetUsd"] != nil {
		t.Fatalf("sub-task node = %v, want id %d without a budget", node, kid)
	}
}
