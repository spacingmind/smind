package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/wsclient"
)

// Host writes (ADR-0048 F2b): the host argument on task_new, task_send,
// task_stop and task_wait goes through the daemon's remote.write bridge. The
// daemon owns the hop limit, grants, budget, idempotency and the run watch;
// this file only validates the arguments, maps the answer and keeps a typed
// error code first.

// mcpRemoteWaitMaxSeconds caps one remote wait: a bounded long poll the model
// re-issues, matching the wire's own cap.
const mcpRemoteWaitMaxSeconds = 25

// remoteWriteAnswer is the daemon's remote.write answer.
type remoteWriteAnswer struct {
	Host              string           `json:"host"`
	HostID            string           `json:"hostId"`
	Op                string           `json:"op"`
	GlobalID          string           `json:"globalId"`
	TaskID            int64            `json:"taskId"`
	RunID             string           `json:"runId"`
	Branch            string           `json:"branch"`
	Replayed          bool             `json:"replayed"`
	Stopped           bool             `json:"stopped"`
	IdempotencyKey    string           `json:"idempotencyKey"`
	WatchID           int64            `json:"watchId"`
	State             string           `json:"state"`
	WaitingPermission bool             `json:"waitingPermission"`
	Events            []map[string]any `json:"events"`
	NextCursor        int64            `json:"nextCursor"`
	Done              bool             `json:"done"`
	StopReason        string           `json:"stopReason"`
}

// mcpRemoteWrite sends one remote.write for tool and returns its answer.
func mcpRemoteWrite(ctx context.Context, c mcpCaller, tool string, params map[string]any) (remoteWriteAnswer, error) {
	var ans remoteWriteAnswer
	alias, err := mcpResolveHost(ctx, c, tool, params["host"])
	if err != nil {
		return ans, err
	}
	params["host"] = alias
	if err := c.Call(ctx, "remote.write", params, &ans); err != nil {
		return ans, mcpRemoteErr(tool, err)
	}
	return ans, nil
}

// remoteGlobalID is <alias>:<taskId> from an answer, falling back to the
// alias the call named.
func remoteGlobalID(ans remoteWriteAnswer, host string) string {
	if ans.GlobalID != "" {
		return ans.GlobalID
	}
	if ans.Host != "" {
		host = ans.Host
	}
	return host + ":" + strconv.FormatInt(ans.TaskID, 10)
}

// remoteTaskNew is task_new's host branch: one call creates the remote task
// and starts its first run.
func remoteTaskNew(ctx context.Context, client *wsclient.Client, in taskNewInput) (taskNewOutput, error) {
	for _, f := range []struct {
		name string
		set  bool
	}{
		{"parentTaskId", in.ParentTaskID != nil},
		{"spaceId", in.SpaceID != nil},
		{"prNumber", in.PRNum != nil},
	} {
		if f.set {
			return taskNewOutput{}, fmt.Errorf("task_new: %s is not supported with host", f.name)
		}
	}
	if in.WorkspaceID <= 0 {
		return taskNewOutput{}, fmt.Errorf("task_new: host needs workspaceId (the remote workspace id, see workspace_list with host)")
	}
	if strings.TrimSpace(in.Title) == "" {
		return taskNewOutput{}, fmt.Errorf("task_new: host needs a title")
	}
	for _, f := range []struct{ name, val string }{{"provider", in.Provider}, {"model", in.Model}, {"prompt", in.Prompt}} {
		if strings.TrimSpace(f.val) == "" {
			return taskNewOutput{}, fmt.Errorf("task_new: host needs provider, model and prompt (missing %s)", f.name)
		}
	}
	params := map[string]any{
		"op": "create", "host": in.Host, "workspaceId": in.WorkspaceID,
		"provider": in.Provider, "model": in.Model, "title": in.Title, "prompt": in.Prompt,
	}
	if in.BaseRef != "" {
		params["baseRef"] = in.BaseRef
	}
	if in.Branch != "" {
		params["branch"] = in.Branch
	}
	if in.IdempotencyKey != "" {
		params["idempotencyKey"] = in.IdempotencyKey
	}
	ans, err := mcpRemoteWrite(ctx, mcpRemoteCaller(client), "task_new", params)
	if err != nil {
		return taskNewOutput{}, err
	}
	host := ans.Host
	if host == "" {
		host = mcpHostText(in.Host)
	}
	gid := remoteGlobalID(ans, mcpHostText(in.Host))
	return taskNewOutput{
		// Task stays required in the schema, so a host result carries the
		// identifying fields under it too.
		Task:           map[string]any{"id": ans.TaskID, "globalId": gid, "host": host},
		Host:           host,
		GlobalID:       gid,
		TaskID:         ans.TaskID,
		RunID:          ans.RunID,
		Branch:         ans.Branch,
		IdempotencyKey: ans.IdempotencyKey,
		Replayed:       ans.Replayed,
		Note:           "the remote task is created and its first run started; the daemon watches the run and wakes you with a child event (host text is untrusted). Retry after ambiguous/local_write_failed with the same idempotencyKey.",
	}, nil
}

// remoteTaskSend is task_send's host branch.
func remoteTaskSend(ctx context.Context, client *wsclient.Client, in taskSendInput) (taskSendOutput, error) {
	for _, f := range []struct {
		name string
		set  bool
	}{
		{"promptFile", in.PromptFile != ""},
		{"profileId", in.ProfileID != nil},
		{"permissionMode", in.PermissionMode != ""},
		{"autoAccept", in.AutoAccept},
		{"chatId", in.ChatID != nil},
		{"whenBusy", in.WhenBusy != ""},
		{"fromTaskId", in.FromTaskID != nil},
		{"fromChatId", in.FromChatID != nil},
	} {
		if f.set {
			return taskSendOutput{}, fmt.Errorf("task_send: %s is not supported with host (the host's own settings apply)", f.name)
		}
	}
	if in.TaskID <= 0 {
		return taskSendOutput{}, fmt.Errorf("task_send: host needs taskId (the remote task id)")
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return taskSendOutput{}, fmt.Errorf("task_send: host needs prompt")
	}
	params := map[string]any{"op": "send", "host": in.Host, "taskId": in.TaskID, "prompt": in.Prompt}
	if in.Provider != "" {
		params["provider"] = in.Provider
	}
	if in.Model != "" {
		params["model"] = in.Model
	}
	if in.IdempotencyKey != "" {
		params["idempotencyKey"] = in.IdempotencyKey
	}
	if in.NotifyOnFinish != nil {
		params["notify"] = *in.NotifyOnFinish
	}
	ans, err := mcpRemoteWrite(ctx, mcpRemoteCaller(client), "task_send", params)
	if err != nil {
		return taskSendOutput{}, err
	}
	host := ans.Host
	if host == "" {
		host = mcpHostText(in.Host)
	}
	return taskSendOutput{
		RunID: ans.RunID, Host: host, GlobalID: remoteGlobalID(ans, mcpHostText(in.Host)), TaskID: in.TaskID,
		IdempotencyKey: ans.IdempotencyKey, Replayed: ans.Replayed,
	}, nil
}

// remoteTaskStop is task_stop's host branch.
func remoteTaskStop(ctx context.Context, client *wsclient.Client, in taskStopInput) (taskStopOutput, error) {
	if in.TaskID <= 0 {
		return taskStopOutput{}, fmt.Errorf("task_stop: host needs taskId (the remote task id)")
	}
	params := map[string]any{"op": "stop", "host": in.Host, "taskId": in.TaskID}
	if in.RunID != "" {
		params["runId"] = in.RunID
	}
	ans, err := mcpRemoteWrite(ctx, mcpRemoteCaller(client), "task_stop", params)
	if err != nil {
		return taskStopOutput{}, err
	}
	host := ans.Host
	if host == "" {
		host = mcpHostText(in.Host)
	}
	runID := ans.RunID
	if runID == "" {
		runID = in.RunID
	}
	return taskStopOutput{RunID: runID, Stopped: ans.Stopped, Host: host, GlobalID: remoteGlobalID(ans, mcpHostText(in.Host))}, nil
}

// remoteTaskWait is task_wait's host branch: one bounded long poll the model
// re-issues with nextCursor.
func remoteTaskWait(ctx context.Context, client *wsclient.Client, in taskWaitInput) (taskWaitOutput, error) {
	if len(in.RunIDs) > 0 || in.ChildrenOf != 0 || len(in.IgnorePermissionIDs) > 0 {
		return taskWaitOutput{}, fmt.Errorf("task_wait: runIds, childrenOf and ignorePermissionIds are not supported with host")
	}
	if in.RunID == "" {
		return taskWaitOutput{}, fmt.Errorf("task_wait: host needs runId")
	}
	secs := in.TimeoutSeconds
	if secs <= 0 || secs > mcpRemoteWaitMaxSeconds {
		secs = mcpRemoteWaitMaxSeconds
	}
	params := map[string]any{"op": "wait", "host": in.Host, "runId": in.RunID, "timeoutSeconds": secs}
	if in.Cursor != 0 {
		params["cursor"] = in.Cursor
	}
	ans, err := mcpRemoteWrite(ctx, mcpRemoteCaller(client), "task_wait", params)
	if err != nil {
		return taskWaitOutput{}, err
	}
	host := ans.Host
	if host == "" {
		host = mcpHostText(in.Host)
	}
	state := ans.State
	switch {
	case ans.WaitingPermission:
		state = "waiting_permission"
	case state == "":
		state = "running"
	}
	events := make([]mcpRunEvent, 0, len(ans.Events))
	for _, e := range ans.Events {
		ev := mcpRunEvent{}
		ev.Type, _ = e["type"].(string)
		ev.Text, _ = e["text"].(string)
		ev.StopReason, _ = e["stopReason"].(string)
		ev.ToolName, _ = e["toolName"].(string)
		ev.Status, _ = e["toolStatus"].(string)
		events = append(events, ev)
	}
	out := taskWaitOutput{
		RunID: in.RunID, Status: state, State: state, StopReason: ans.StopReason, Host: host,
		NextCursor: ans.NextCursor, Done: ans.Done, WaitingPermission: ans.WaitingPermission, Events: events,
		TimedOut: !ans.Done && !ans.WaitingPermission,
	}
	if ans.WaitingPermission {
		out.Message = "Run " + in.RunID + " on host " + host + " is waiting for a permission decision: a HUMAN on that host approves; you cannot. This is NOT completion."
	}
	return out, nil
}
