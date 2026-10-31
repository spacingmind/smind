package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

func writeAnswer(op string) map[string]any {
	return map[string]any{
		"host": "mac", "hostId": "h1", "op": op, "globalId": "mac:7", "taskId": 7,
		"runId": "r9", "branch": "smind/x", "idempotencyKey": "k1", "stopped": op == "stop",
	}
}

func TestMCPHostTaskNewRequiresProviderModelPrompt(t *testing.T) {
	ctx := context.Background()
	base := taskNewInput{WorkspaceID: 3, Title: "T", Host: "mac", Provider: "glm", Model: "m", Prompt: "p"}
	for _, field := range []string{"provider", "model", "prompt"} {
		f := &fakeRemoteCaller{answer: writeAnswer("create")}
		useFakeRemote(t, f)
		in := base
		switch field {
		case "provider":
			in.Provider = ""
		case "model":
			in.Model = ""
		case "prompt":
			in.Prompt = ""
		}
		_, _, err := mcpTaskNew(nil)(ctx, nil, in)
		if err == nil || !strings.Contains(err.Error(), "missing "+field) || !strings.Contains(err.Error(), "provider, model and prompt") {
			t.Errorf("%s: err = %v", field, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: made %d calls, want none", field, len(f.calls))
		}
	}

	f := &fakeRemoteCaller{answer: writeAnswer("create")}
	useFakeRemote(t, f)
	_, out, err := mcpTaskNew(nil)(ctx, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	if out.Host != "mac" || out.GlobalID != "mac:7" || out.TaskID != 7 || out.RunID != "r9" || out.IdempotencyKey != "k1" || out.Task["globalId"] != "mac:7" {
		t.Fatalf("out = %+v", out)
	}
	if len(f.calls) != 1 || f.calls[0].method != "remote.write" {
		t.Fatalf("calls = %+v", f.calls)
	}
	p := f.calls[0].params
	if p["op"] != "create" || p["host"] != "mac" || p["workspaceId"] != int64(3) || p["provider"] != "glm" || p["model"] != "m" || p["title"] != "T" || p["prompt"] != "p" {
		t.Fatalf("params = %+v", p)
	}

	parent := int64(1)
	in := base
	in.ParentTaskID = &parent
	if _, _, err := mcpTaskNew(nil)(ctx, nil, in); err == nil || err.Error() != "task_new: parentTaskId is not supported with host" {
		t.Fatalf("parentTaskId err = %v", err)
	}
}

func TestMCPRemoteWriteTypedErrorsCodeFirst(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func() error{
		"task_new": func() error {
			_, _, err := mcpTaskNew(nil)(ctx, nil, taskNewInput{WorkspaceID: 1, Title: "T", Host: "mac", Provider: "p", Model: "m", Prompt: "x"})
			return err
		},
		"task_send": func() error {
			_, _, err := mcpTaskSend(nil)(ctx, nil, taskSendInput{TaskID: 1, Prompt: "x", Host: "mac"})
			return err
		},
		"task_stop": func() error {
			_, _, err := mcpTaskStop(nil)(ctx, nil, taskStopInput{TaskID: 1, Host: "mac"})
			return err
		},
		"task_wait": func() error {
			_, _, err := mcpTaskWait(nil)(ctx, nil, taskWaitInput{RunID: "r", Host: "mac"})
			return err
		},
	}
	for tool, call := range calls {
		for _, code := range []string{"budget_exceeded", "hop_limit", "not_granted"} {
			useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Code: code, Message: "nope " + code}})
			if err := call(); err == nil || !strings.HasPrefix(err.Error(), tool+": "+code+":") {
				t.Errorf("%s %s: err = %v", tool, code, err)
			}
		}
		msg := "outcome unknown; retry with idempotencyKey k1"
		useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Code: "ambiguous", Message: msg}})
		if err := call(); err == nil || !strings.HasPrefix(err.Error(), tool+": ambiguous:") || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s ambiguous: err = %v", tool, err)
		}
	}
}

func TestMCPHostSendStopWaitMapToRemoteWrite(t *testing.T) {
	ctx := context.Background()

	f := &fakeRemoteCaller{answer: writeAnswer("send")}
	useFakeRemote(t, f)
	_, so, err := mcpTaskSend(nil)(ctx, nil, taskSendInput{TaskID: 7, Prompt: "go", Host: "mac", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if so.RunID != "r9" || so.Host != "mac" || so.GlobalID != "mac:7" || so.TaskID != 7 || so.IdempotencyKey != "k1" {
		t.Fatalf("send out = %+v", so)
	}
	if p := f.calls[0].params; f.calls[0].method != "remote.write" || p["op"] != "send" || p["taskId"] != int64(7) || p["prompt"] != "go" || p["idempotencyKey"] != "k1" {
		t.Fatalf("send params = %+v", f.calls[0])
	}

	f = &fakeRemoteCaller{answer: writeAnswer("stop")}
	useFakeRemote(t, f)
	_, st, err := mcpTaskStop(nil)(ctx, nil, taskStopInput{TaskID: 7, RunID: "r9", Host: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	if !st.Stopped || st.RunID != "r9" || st.Host != "mac" || st.GlobalID != "mac:7" {
		t.Fatalf("stop out = %+v", st)
	}
	if p := f.calls[0].params; p["op"] != "stop" || p["taskId"] != int64(7) || p["runId"] != "r9" {
		t.Fatalf("stop params = %+v", p)
	}

	wa := writeAnswer("wait")
	wa["events"] = []map[string]any{{"seq": 4, "type": "agent_text", "text": "hi"}}
	wa["nextCursor"], wa["state"] = 5, "running"
	f = &fakeRemoteCaller{answer: wa}
	useFakeRemote(t, f)
	_, wo, err := mcpTaskWait(nil)(ctx, nil, taskWaitInput{RunID: "r9", Host: "mac", Cursor: 3, TimeoutSeconds: 999})
	if err != nil {
		t.Fatal(err)
	}
	if p := f.calls[0].params; p["op"] != "wait" || p["runId"] != "r9" || p["cursor"] != int64(3) || p["timeoutSeconds"] != 25 {
		t.Fatalf("wait params = %+v", p)
	}
	if wo.State != "running" || !wo.TimedOut || wo.Done || wo.NextCursor != 5 || len(wo.Events) != 1 || wo.Events[0].Text != "hi" || wo.Host != "mac" {
		t.Fatalf("wait out = %+v", wo)
	}

	f = &fakeRemoteCaller{}
	useFakeRemote(t, f)
	pid := int64(2)
	if _, _, err := mcpTaskSend(nil)(ctx, nil, taskSendInput{TaskID: 7, PromptFile: "p.md", Host: "mac"}); err == nil || !strings.Contains(err.Error(), "task_send: promptFile is not supported with host") {
		t.Errorf("promptFile err = %v", err)
	}
	if _, _, err := mcpTaskSend(nil)(ctx, nil, taskSendInput{TaskID: 7, Prompt: "x", ProfileID: &pid, Host: "mac"}); err == nil || !strings.Contains(err.Error(), "profileId is not supported with host") {
		t.Errorf("profileId err = %v", err)
	}
	if _, _, err := mcpTaskWait(nil)(ctx, nil, taskWaitInput{RunIDs: []string{"a", "b"}, Host: "mac"}); err == nil || !strings.Contains(err.Error(), "runIds") {
		t.Errorf("runIds err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("made %d calls, want none", len(f.calls))
	}
}

func TestMCPWriteToolsLocalUnchanged(t *testing.T) {
	f := &fakeRemoteCaller{err: &wsclient.RPCError{Code: "boom", Message: "must not be called"}}
	useFakeRemote(t, f)
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)
	wsID := createTestWorkspace(t, client)

	var created struct {
		Task map[string]any `json:"task"`
	}
	if callMCPTool(t, env.cs, "task_new", map[string]any{"workspaceId": wsID, "title": "T"}, &created) {
		t.Fatal("task_new failed")
	}
	if created.Task["ID"] == nil {
		t.Fatalf("task = %+v", created.Task)
	}
	// Local sends/stops/waits on a missing run fail daemon-side, never remote.
	for tool, args := range map[string]map[string]any{
		"task_send": {"taskId": 1, "prompt": "x", "provider": "glm"},
		"task_stop": {"runId": "nope"},
		"task_wait": {"runId": "nope", "timeoutSeconds": 1},
	} {
		_, _ = env.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	}
	res, err := env.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "task_stop", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(mcpToolErrorText(res), "runId is required") {
		t.Fatalf("task_stop without runId = %q", mcpToolErrorText(res))
	}
	for _, c := range f.calls {
		if c.method == "remote.write" {
			t.Fatalf("local call reached remote.write: %+v", c)
		}
	}
}

func TestFleetPeerSeatGetsNoFleetTools(t *testing.T) {
	has := func(t *testing.T) bool {
		env := newMCPSession(t)
		return slices.Contains(mcpToolNames(t, env.cs), "list_hosts")
	}

	t.Setenv("SMIND_RUN_ID", "run-1")
	useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Code: "hop_limit", Message: "peer seat"}})
	if has(t) {
		t.Error("peer seat with hop_limit still has list_hosts")
	}

	useFakeRemote(t, &fakeRemoteCaller{answer: map[string]any{"hosts": []any{}}})
	if !has(t) {
		t.Error("peer seat with a normal answer lost list_hosts")
	}

	useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Message: "method not found: remote.hosts"}})
	if !has(t) {
		t.Error("peer seat with fleet off lost list_hosts")
	}

	t.Setenv("SMIND_RUN_ID", "")
	useFakeRemote(t, &fakeRemoteCaller{err: &wsclient.RPCError{Code: "hop_limit", Message: "x"}})
	if !has(t) {
		t.Error("non-seat lost list_hosts")
	}
}
