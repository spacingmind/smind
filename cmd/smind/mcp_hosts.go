package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spacingmind/smind/internal/wsclient"
)

// Host reads (ADR-0048 F2b): list_hosts and the optional host argument on the
// read tools go through the daemon's remote.hosts / remote.read bridge. The
// daemon enforces hop limit 1 and grants; this file only maps the envelope and
// puts a typed error code first so the model can read it.

// mcpCaller is the one wsapi call the host reads need; *wsclient.Client
// satisfies it.
type mcpCaller interface {
	Call(ctx context.Context, method string, params, out any) error
}

// mcpRemoteCaller is the seam tests override to fake the daemon's bridge.
var mcpRemoteCaller = func(c *wsclient.Client) mcpCaller { return c }

// remoteEnvelope is the daemon's remote.read answer.
type remoteEnvelope struct {
	Host          string          `json:"host"`
	HostID        string          `json:"hostId"`
	Op            string          `json:"op"`
	Data          json.RawMessage `json:"data"`
	StatusUnknown bool            `json:"statusUnknown"`
	Failures      int             `json:"failures"`
}

const mcpFleetOffText = "host_unavailable: fleet is off on this daemon (set fleet.enabled in config.yaml); host reads need it"

// mcpRemoteErr maps a bridge failure to a tool error with the typed code first.
func mcpRemoteErr(tool string, err error) error {
	var rpc *wsclient.RPCError
	if errors.As(err, &rpc) {
		if strings.Contains(rpc.Message, "method not found") {
			return fmt.Errorf("%s: %s", tool, mcpFleetOffText)
		}
		if rpc.Code != "" {
			if strings.HasPrefix(rpc.Message, rpc.Code+":") {
				return fmt.Errorf("%s: %s", tool, rpc.Message)
			}
			return fmt.Errorf("%s: %s: %s", tool, rpc.Code, rpc.Message)
		}
	}
	return fmt.Errorf("%s: %w", tool, err)
}

// mcpHostSet reports whether a host argument was given (nil and "" mean local).
func mcpHostSet(v any) bool { return v != nil && v != "" }

// mcpHostText is the host argument as an alias string, "" for a selector.
func mcpHostText(v any) string {
	s, _ := v.(string)
	return s
}

// mcpResolveHost turns the host argument into an alias: a string is returned
// as is, a label selector costs one remote.select (never retried).
func mcpResolveHost(ctx context.Context, c mcpCaller, tool string, host any) (string, error) {
	switch h := host.(type) {
	case string:
		return h, nil
	case map[string]any:
		bad := fmt.Errorf("%s: host selector must be {\"labels\":{...}} with optional \"any\":true", tool)
		labels, ok := h["labels"].(map[string]any)
		if !ok || len(labels) == 0 {
			return "", bad
		}
		for _, v := range labels {
			if _, ok := v.(string); !ok {
				return "", bad
			}
		}
		for k, v := range h {
			if k == "labels" {
				continue
			}
			if _, isBool := v.(bool); k != "any" || !isBool {
				return "", bad
			}
		}
		params := map[string]any{"labels": labels, "any": h["any"] == true}
		var res struct {
			Host   string `json:"host"`
			HostID string `json:"hostId"`
		}
		if err := c.Call(ctx, "remote.select", params, &res); err != nil {
			return "", mcpRemoteErr(tool, err)
		}
		return res.Host, nil
	}
	return "", fmt.Errorf("%s: host selector must be {\"labels\":{...}} with optional \"any\":true", tool)
}

// mcpRemoteRead sends one remote.read for tool and returns its envelope.
func mcpRemoteRead(ctx context.Context, c mcpCaller, tool string, params map[string]any) (remoteEnvelope, error) {
	var env remoteEnvelope
	alias, err := mcpResolveHost(ctx, c, tool, params["host"])
	if err != nil {
		return env, err
	}
	params["host"] = alias
	if err := c.Call(ctx, "remote.read", params, &env); err != nil {
		return env, mcpRemoteErr(tool, err)
	}
	return env, nil
}

func registerMCPHostTools(srv *mcp.Server, client *wsclient.Client) {
	// A peer seat (a run on this daemon) that the daemon refuses host calls
	// for must not even see the tool. Any other probe error keeps it.
	if os.Getenv("SMIND_RUN_ID") != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := mcpRemoteCaller(client).Call(ctx, "remote.hosts", nil, &struct{}{})
		cancel()
		var rpc *wsclient.RPCError
		if errors.As(err, &rpc) && rpc.Code == "hop_limit" {
			return
		}
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "list_hosts", Description: "List the connected peer hosts (alias, hostId, pairing state, link state). A host argument on workspace_list, task_list, task_get, task_status and task_logs reads that host instead of this daemon; on task_new, task_send, task_stop and task_wait it acts on that host. Direct only (hop limit 1): a task that was itself created by a peer host cannot use hosts and gets hop_limit. Peer seats and tasks created by a peer host get no host tools: the daemon refuses their host calls with hop_limit. Never put the secrets of one host in a prompt for another."}, mcpListHosts(client))
}

type listHostsInput struct{}

type hostView struct {
	Alias  string `json:"alias"`
	HostID string `json:"hostId"`
	State  string `json:"state"`
	Link   string `json:"link"`
}

type listHostsOutput struct {
	Hosts []hostView `json:"hosts"`
	Fleet string     `json:"fleet,omitempty"`
}

func mcpListHosts(client *wsclient.Client) mcp.ToolHandlerFor[listHostsInput, listHostsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ listHostsInput) (*mcp.CallToolResult, listHostsOutput, error) {
		var res struct {
			Hosts []hostView `json:"hosts"`
		}
		if err := mcpRemoteCaller(client).Call(ctx, "remote.hosts", nil, &res); err != nil {
			var rpc *wsclient.RPCError
			if errors.As(err, &rpc) && strings.Contains(rpc.Message, "method not found") {
				return nil, listHostsOutput{Hosts: []hostView{}, Fleet: "off"}, nil
			}
			return nil, listHostsOutput{}, mcpRemoteErr("list_hosts", err)
		}
		if res.Hosts == nil {
			res.Hosts = []hostView{}
		}
		return nil, listHostsOutput{Hosts: res.Hosts}, nil
	}
}

// remoteWorkspaceList is workspace_list's host branch.
func remoteWorkspaceList(ctx context.Context, client *wsclient.Client, host any) (workspaceListOutput, error) {
	env, err := mcpRemoteRead(ctx, mcpRemoteCaller(client), "workspace_list", map[string]any{"op": "workspaces", "host": host})
	if err != nil {
		return workspaceListOutput{}, err
	}
	var d struct {
		Workspaces []map[string]any `json:"workspaces"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return workspaceListOutput{}, fmt.Errorf("workspace_list: bad answer: %w", err)
	}
	if d.Workspaces == nil {
		d.Workspaces = []map[string]any{}
	}
	for _, w := range d.Workspaces {
		w["host"] = env.Host
		w["globalId"] = env.Host + ":" + mcpAnyID(w["id"])
	}
	return workspaceListOutput{Workspaces: d.Workspaces, Host: env.Host}, nil
}

// mcpAnyID renders a decoded JSON number id without an exponent.
func mcpAnyID(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprint(v)
}

// remoteTaskList is task_list's host branch.
func remoteTaskList(ctx context.Context, client *wsclient.Client, in taskListInput) (taskListOutput, error) {
	params := map[string]any{"op": "tasks", "host": in.Host}
	if in.WorkspaceID != 0 {
		params["workspaceId"] = in.WorkspaceID
	}
	if in.Cursor != 0 {
		params["cursor"] = in.Cursor
	}
	if in.Limit != 0 {
		params["limit"] = in.Limit
	}
	env, err := mcpRemoteRead(ctx, mcpRemoteCaller(client), "task_list", params)
	if err != nil {
		return taskListOutput{}, err
	}
	var d struct {
		Tasks      []map[string]any `json:"tasks"`
		NextCursor int64            `json:"nextCursor"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return taskListOutput{}, fmt.Errorf("task_list: bad answer: %w", err)
	}
	if d.Tasks == nil {
		d.Tasks = []map[string]any{}
	}
	return taskListOutput{Tasks: d.Tasks, NextCursor: d.NextCursor, Host: env.Host}, nil
}

// remoteTaskGet is task_get's host branch.
func remoteTaskGet(ctx context.Context, client *wsclient.Client, in taskGetInput) (taskGetOutput, error) {
	env, err := mcpRemoteRead(ctx, mcpRemoteCaller(client), "task_get", map[string]any{"op": "task", "host": in.Host, "id": in.ID})
	if err != nil {
		return taskGetOutput{}, err
	}
	var task map[string]any
	if err := json.Unmarshal(env.Data, &task); err != nil {
		return taskGetOutput{}, fmt.Errorf("task_get: bad answer: %w", err)
	}
	return taskGetOutput{Task: task}, nil
}

// remoteTaskStatus is task_status's host branch.
func remoteTaskStatus(ctx context.Context, client *wsclient.Client, in taskStatusInput) (taskStatusOutput, error) {
	if in.RunID == "" && in.TaskID == 0 {
		return taskStatusOutput{}, fmt.Errorf("task_status: host reads need runId or taskId")
	}
	params := map[string]any{"op": "status", "host": in.Host}
	if in.RunID != "" {
		params["runId"] = in.RunID
	}
	if in.TaskID != 0 {
		params["taskId"] = in.TaskID
	}
	env, err := mcpRemoteRead(ctx, mcpRemoteCaller(client), "task_status", params)
	if err != nil {
		return taskStatusOutput{}, err
	}
	out := taskStatusOutput{RunID: in.RunID, Events: []mcpRunEvent{}, Host: env.Host}
	if in.TaskID != 0 {
		out.GlobalID = env.Host + ":" + strconv.FormatInt(in.TaskID, 10)
	}
	if env.StatusUnknown {
		out.Status = "status_unknown"
		out.StatusUnknown = true
		out.Note = fmt.Sprintf("could not observe the run after %d consecutive attempts; its status is unknown (it may still be running or may have finished). Inspect the host before re-delegating. This is not a finished state.", env.Failures)
		return out, nil
	}
	var d struct {
		RunID      string   `json:"runId"`
		TaskID     int64    `json:"taskId"`
		State      string   `json:"state"`
		StopReason string   `json:"stopReason"`
		Failure    string   `json:"failureKind"`
		CostUSD    *float64 `json:"costUsd"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return taskStatusOutput{}, fmt.Errorf("task_status: bad answer: %w", err)
	}
	if d.RunID != "" {
		out.RunID = d.RunID
	}
	if d.TaskID != 0 {
		out.GlobalID = env.Host + ":" + strconv.FormatInt(d.TaskID, 10)
	}
	out.Status, out.StopReason, out.FailureKind, out.CostUSD = d.State, d.StopReason, d.Failure, d.CostUSD
	return out, nil
}

// remoteTaskLogs is task_logs's host branch.
func remoteTaskLogs(ctx context.Context, client *wsclient.Client, in taskLogsInput) (taskLogsOutput, error) {
	if in.TaskID == 0 {
		return taskLogsOutput{}, fmt.Errorf("task_logs: host reads need taskId")
	}
	params := map[string]any{"op": "activity", "host": in.Host, "taskId": in.TaskID}
	if in.RunID != "" {
		params["runId"] = in.RunID
	}
	if in.Cursor != 0 {
		params["cursor"] = in.Cursor
	}
	env, err := mcpRemoteRead(ctx, mcpRemoteCaller(client), "task_logs", params)
	if err != nil {
		return taskLogsOutput{}, err
	}
	var d struct {
		Events []struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			StopReason string `json:"stopReason"`
		} `json:"events"`
		NextCursor int64 `json:"nextCursor"`
		HasMore    bool  `json:"hasMore"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return taskLogsOutput{}, fmt.Errorf("task_logs: bad answer: %w", err)
	}
	events := make([]mcpRunEvent, 0, len(d.Events))
	for _, e := range d.Events {
		events = append(events, mcpRunEvent{Type: e.Type, Text: e.Text, StopReason: e.StopReason})
	}
	return taskLogsOutput{RunID: in.RunID, Status: "remote", Events: events, Host: env.Host, NextCursor: d.NextCursor, HasMore: d.HasMore}, nil
}
