package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// fakeRemoteCaller answers remote.read / remote.hosts from canned values and
// records every call.
type fakeRemoteCaller struct {
	answer any   // marshalled into out
	err    error // returned instead of answer
	calls  []fakeRemoteCall
}

type fakeRemoteCall struct {
	method string
	params map[string]any
}

func (f *fakeRemoteCaller) Call(_ context.Context, method string, params, out any) error {
	p, _ := params.(map[string]any)
	f.calls = append(f.calls, fakeRemoteCall{method: method, params: p})
	if f.err != nil {
		return f.err
	}
	raw, err := json.Marshal(f.answer)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// useFakeRemote routes the host branches to f for the test.
func useFakeRemote(t *testing.T, f *fakeRemoteCaller) {
	t.Helper()
	old := mcpRemoteCaller
	mcpRemoteCaller = func(*wsclient.Client) mcpCaller { return f }
	t.Cleanup(func() { mcpRemoteCaller = old })
}

func envelope(op string, data any) map[string]any {
	return map[string]any{"host": "mac", "hostId": "h1", "op": op, "data": data}
}

func TestMCPListHostsAndHostParamLocalUnchanged(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	wsID := createTestWorkspace(t, client)

	var created struct {
		Task struct {
			ID int64 `json:"ID"`
		} `json:"task"`
	}
	if callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": wsID, "title": "T"}, &created) {
		t.Fatal("task_new failed")
	}

	var gotWS struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if callMCPTool(t, env.cs, "workspace_list", map[string]any{}, &gotWS) {
		t.Fatal("workspace_list failed")
	}
	var wantWS []map[string]any
	if err := client.Call(context.Background(), "workspace.list", nil, &wantWS); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotWS.Workspaces, wantWS) {
		t.Fatalf("workspace_list = %v, want %v", gotWS.Workspaces, wantWS)
	}

	var gotTasks struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if callMCPTool(t, env.cs, "task_list", map[string]any{"workspaceId": wsID}, &gotTasks) {
		t.Fatal("task_list failed")
	}
	var wantTasks []map[string]any
	if err := client.Call(context.Background(), "task.list", map[string]any{"workspaceId": wsID}, &wantTasks); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotTasks.Tasks, wantTasks) {
		t.Fatalf("task_list = %v, want %v", gotTasks.Tasks, wantTasks)
	}

	var gotTask struct {
		Task map[string]any `json:"task"`
	}
	if callMCPTool(t, env.cs, "task_get", map[string]any{"id": created.Task.ID}, &gotTask) {
		t.Fatal("task_get failed")
	}
	var wantTask map[string]any
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": created.Task.ID}, &wantTask); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotTask.Task, wantTask) {
		t.Fatalf("task_get = %v, want %v", gotTask.Task, wantTask)
	}

	var hosts struct {
		Hosts []map[string]any `json:"hosts"`
		Fleet string           `json:"fleet"`
	}
	if callMCPTool(t, env.cs, "list_hosts", map[string]any{}, &hosts) {
		t.Fatal("list_hosts failed")
	}
	if hosts.Hosts == nil || len(hosts.Hosts) != 0 || hosts.Fleet != "off" {
		t.Fatalf("list_hosts = %+v, want empty hosts and fleet off", hosts)
	}

	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "workspace_list", Arguments: map[string]any{"host": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(mcpToolErrorText(res), "fleet is off") {
		t.Fatalf("workspace_list host=x = %q (isError %v), want a fleet is off error", mcpToolErrorText(res), res.IsError)
	}
}

func TestMCPRemoteReadsEnvelopeAndGlobalIDs(t *testing.T) {
	ctx := context.Background()

	f := &fakeRemoteCaller{answer: envelope("workspaces", map[string]any{"workspaces": []map[string]any{{"id": 7, "title": "W"}}})}
	useFakeRemote(t, f)
	_, ws, err := mcpWorkspaceList(nil)(ctx, nil, workspaceListInput{Host: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Host != "mac" || len(ws.Workspaces) != 1 || ws.Workspaces[0]["host"] != "mac" || ws.Workspaces[0]["globalId"] != "mac:7" {
		t.Fatalf("workspace_list = %+v", ws)
	}
	if f.calls[0].method != "remote.read" || f.calls[0].params["op"] != "workspaces" || f.calls[0].params["host"] != "mac" {
		t.Fatalf("params = %+v", f.calls[0])
	}

	f = &fakeRemoteCaller{answer: envelope("tasks", map[string]any{
		"tasks":      []map[string]any{{"id": 3, "host": "mac", "globalId": "mac:3"}},
		"nextCursor": 9,
	})}
	useFakeRemote(t, f)
	_, tl, err := mcpTaskList(nil)(ctx, nil, taskListInput{WorkspaceID: 5, Host: "mac", Cursor: 2, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if tl.Host != "mac" || tl.NextCursor != 9 || tl.Tasks[0]["globalId"] != "mac:3" {
		t.Fatalf("task_list = %+v", tl)
	}
	p := f.calls[0].params
	if p["op"] != "tasks" || p["workspaceId"] != int64(5) || p["cursor"] != int64(2) || p["limit"] != 10 {
		t.Fatalf("params = %+v", p)
	}

	f = &fakeRemoteCaller{answer: envelope("task", map[string]any{"id": 4, "host": "mac", "globalId": "mac:4"})}
	useFakeRemote(t, f)
	_, tg, err := mcpTaskGet(nil)(ctx, nil, taskGetInput{ID: 4, Host: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Task["host"] != "mac" || tg.Task["globalId"] != "mac:4" {
		t.Fatalf("task_get = %+v", tg)
	}
	if p := f.calls[0].params; p["op"] != "task" || p["id"] != int64(4) {
		t.Fatalf("params = %+v", p)
	}
}

func TestMCPRemoteStatusUnknownNeverDone(t *testing.T) {
	e := envelope("status", nil)
	e["statusUnknown"], e["failures"] = true, 3
	f := &fakeRemoteCaller{answer: e}
	useFakeRemote(t, f)
	_, out, err := mcpTaskStatus(nil)(context.Background(), nil, taskStatusInput{RunID: "r1", Host: "mac", TaskID: 4})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "status_unknown" || !out.StatusUnknown || out.Note == "" {
		t.Fatalf("out = %+v", out)
	}
	raw, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for _, bad := range []string{"done", "finished", "stopped"} {
		if m["status"] == bad {
			t.Fatalf("status = %q", bad)
		}
	}
	if !strings.Contains(out.Note, "3 consecutive attempts") || !strings.Contains(out.Note, "not a finished state") {
		t.Fatalf("note = %q", out.Note)
	}
	if f.calls[0].params["op"] != "status" || f.calls[0].params["taskId"] != int64(4) {
		t.Fatalf("params = %+v", f.calls[0].params)
	}
}

func TestMCPRemoteTypedErrorsCodeFirst(t *testing.T) {
	for _, code := range []string{"hop_limit", "not_granted", "fleet_version_unsupported", "cycle"} {
		useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Code: code, Message: "nope " + code}})
		_, _, err := mcpTaskStatus(nil)(context.Background(), nil, taskStatusInput{RunID: "r1", Host: "mac"})
		if err == nil || !strings.HasPrefix(err.Error(), "task_status: "+code+":") || !strings.Contains(err.Error(), "nope "+code) {
			t.Errorf("code %s: err = %v", code, err)
		}
	}
	useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Message: "method not found: remote.read"}})
	_, _, err := mcpTaskGet(nil)(context.Background(), nil, taskGetInput{ID: 1, Host: "mac"})
	if err == nil || !strings.HasPrefix(err.Error(), "task_get: host_unavailable: fleet is off") {
		t.Errorf("method not found: err = %v", err)
	}
}

func TestMCPTaskLogsHostNeedsTaskID(t *testing.T) {
	f := &fakeRemoteCaller{}
	useFakeRemote(t, f)
	_, _, err := mcpTaskLogs(nil)(context.Background(), nil, taskLogsInput{Host: "mac", RunID: "r1"})
	if err == nil || err.Error() != "task_logs: host reads need taskId" {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("made %d calls, want none", len(f.calls))
	}

	f = &fakeRemoteCaller{answer: envelope("activity", map[string]any{
		"events":     []map[string]any{{"seq": 1, "type": "agent_text", "text": "hi"}},
		"nextCursor": 5, "hasMore": true,
	})}
	useFakeRemote(t, f)
	_, out, err := mcpTaskLogs(nil)(context.Background(), nil, taskLogsInput{Host: "mac", TaskID: 4, Cursor: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "remote" || out.Host != "mac" || out.NextCursor != 5 || !out.HasMore || len(out.Events) != 1 || out.Events[0].Text != "hi" {
		t.Fatalf("out = %+v", out)
	}
}

func TestMCPHostParamHasHopLimitText(t *testing.T) {
	env := newMCPSession(t)
	res, err := env.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "task_status" {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var s struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if d := s.Properties["host"].Description; !strings.Contains(d, "hop limit") {
			t.Fatalf("host description = %q", d)
		}
		return
	}
	t.Fatal("task_status not listed")
}
