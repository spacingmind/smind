package main

import (
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

// startRichRun starts the fake agent's "structured-permission" run (every
// run.logs event type), resolves its permission and waits for it to finish.
func startRichRun(t *testing.T, env *mcpTestEnv) string {
	t.Helper()
	runID := startTestRun(t, env.srvURL, "glm", "structured-permission")
	client := dialTestClient(t, env.srvURL)
	resolvePendingPermission(t, client, runID)
	waitForRunStatus(t, client, runID, "done")
	return runID
}

func resolvePendingPermission(t *testing.T, client *wsclient.Client, runID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		result, err := mcpRunLogs(t.Context(), client, "test", runID, 0)
		if err == nil {
			if pending := pendingPermissionsFrom(result); len(pending) > 0 {
				if err := client.Call(t.Context(), "run.respondPermission", map[string]any{
					"runId": runID, "requestId": pending[0].RequestID, "optionId": "allow-1",
				}, nil); err != nil {
					t.Fatalf("respondPermission: %v", err)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending permission appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertRichEvents(t *testing.T, events []mcpRunEvent) {
	t.Helper()
	types := map[string]bool{}
	for _, e := range events {
		types[e.Type] = true
		switch e.Type {
		case "tool_call":
			if in, ok := e.Input.(map[string]any); ok && in["command"] == "go test ./..." {
				types["tool_call.input"] = true
			}
			if e.Result != nil {
				types["tool_call.result"] = true
			}
		case "raw":
			if p, ok := e.Payload.(map[string]any); ok && p["entries"] != nil {
				types["raw.payload"] = true
			}
		}
	}
	for _, want := range []string{"tool_call", "tool_call.input", "tool_call.result", "permission_request", "permission_resolved", "chunk", "thinking", "raw", "raw.payload"} {
		if !types[want] {
			t.Errorf("events missing %q; got types %v", want, types)
		}
	}
}

func TestMCPRoundTrip_TaskStatusWithEvents(t *testing.T) {
	env := newMCPSession(t)
	runID := startRichRun(t, env)
	var out taskStatusOutput
	if isErr := callMCPTool(t, env.cs, "task_status", map[string]any{"runId": runID}, &out); isErr {
		t.Fatal("task_status returned an error result")
	}
	assertRichEvents(t, out.Events)
}

func TestMCPRoundTrip_TaskLogsWithEvents(t *testing.T) {
	env := newMCPSession(t)
	runID := startRichRun(t, env)
	var out taskLogsOutput
	if isErr := callMCPTool(t, env.cs, "task_logs", map[string]any{"runId": runID}, &out); isErr {
		t.Fatal("task_logs returned an error result")
	}
	assertRichEvents(t, out.Events)
}

func TestMCPRunEventOutputSchema_JSONFieldsAreNotArrays(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "task_status" && tool.Name != "task_logs" {
			continue
		}
		schema, _ := tool.OutputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		events, _ := props["events"].(map[string]any)
		items, _ := events["items"].(map[string]any)
		itemProps, _ := items["properties"].(map[string]any)
		for _, field := range []string{"input", "result", "payload"} {
			raw, present := itemProps[field]
			if !present {
				t.Fatalf("%s: events.items.properties.%s missing from schema", tool.Name, field)
			}
			// An `any` field infers the accept-everything schema (true or
			// {}), which is exactly what we want.
			fs, _ := raw.(map[string]any)
			if typ, has := fs["type"]; has {
				if typ == "array" {
					t.Errorf("%s: %s declares type array", tool.Name, field)
				}
				if l, ok := typ.([]any); ok && len(l) == 2 && ((l[0] == "null" && l[1] == "array") || (l[0] == "array" && l[1] == "null")) {
					t.Errorf("%s: %s declares type null/array only", tool.Name, field)
				}
			}
		}
	}
}
