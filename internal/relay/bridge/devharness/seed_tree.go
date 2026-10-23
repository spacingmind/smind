package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/tasktree"
	"github.com/spacingmind/smind/internal/workspace"
)

// bigChildren is how many live children the "big" shape gives its root, more
// than the mobile's 100-node render cap.
const bigChildren = 130

// seedNode makes one task of a seeded tree; parent == 0 is a root.
func (h *harness) seedNode(title, scenario string, parent int64) (store.Task, error) {
	var opts []workspace.CreateTaskOption
	if parent != 0 {
		opts = append(opts, workspace.WithParentTask(parent))
	}
	_, task, err := h.newSeedTask(title, scenario, opts...)
	return task, err
}

// startSeedRun starts a fake-agent run on the task and returns its id.
func (h *harness) startSeedRun(taskID int64) (string, error) {
	return h.api.Runs.Start(context.Background(), h.wm, h.runner, taskID, 0, taskrunner.ProviderGLM, "seed", taskrunner.PermissionSettings{}, taskrunner.ThinkingLevelUnspecified)
}

// waitSeedRun polls until ok(status) holds for the run, or fails on timeout.
func (h *harness) waitSeedRun(runID string, ok func(runs.Status) bool) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, st, err := h.api.Runs.History(runID)
		if err != nil {
			return err
		}
		if ok(st.Status) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for run %s (status %s)", runID, st.Status)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func isDone(s runs.Status) bool    { return s == runs.StatusDone }
func isError(s runs.Status) bool   { return s == runs.StatusError }
func isRunning(s runs.Status) bool { return s == runs.StatusRunning }

// seedRunOn gives the task one run with the given scenario and waits for want.
func (h *harness) seedRunOn(task store.Task, scenario string, want func(runs.Status) bool) (string, error) {
	if scenario != "" {
		if err := h.writeScenario(task, scenario); err != nil {
			return "", err
		}
	}
	runID, err := h.startSeedRun(task.ID)
	if err != nil {
		return "", err
	}
	return runID, h.waitSeedRun(runID, want)
}

// seedCost records a known cost for a finished run, as the daemon would from
// the agent's usage report; runs without it have unknown (n/a) cost.
func (h *harness) seedCost(runID string, taskID int64, usd float64) error {
	in, out := int64(1200), int64(340)
	model := "seed"
	return h.store.UpsertRunUsage(store.RunUsage{
		RunID: runID, TaskID: taskID, Provider: string(taskrunner.ProviderGLM), Model: &model,
		InputTokens: &in, OutputTokens: &out, CostUSD: &usd, Source: "seed",
	})
}

// seedTree builds a fresh task tree under the shared seeded workspace for the
// mobile task-tree smoke (TEST ONLY) and answers the root task id plus every
// node id by name.
func (h *harness) seedTree(args []string) (map[string]any, error) {
	if h.runner == nil {
		return nil, errors.New("seed-tree needs -fake-agent")
	}
	if err := need(args, 1, "seed-tree <nested|permission|ratelimited|failed|big>"); err != nil {
		return nil, err
	}
	shape := args[0]
	nodes := map[string]int64{}
	add := func(name, scenario string, parent string) (store.Task, error) {
		t, err := h.seedNode(name, scenario, nodes[parent])
		if err == nil {
			nodes[name] = t.ID
		}
		return t, err
	}
	reply := func(extra map[string]any) map[string]any {
		out := map[string]any{"shape": shape, "rootId": nodes["supervisor"], "workspaceId": h.seedWS.ID, "nodes": nodes}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	root, err := add("supervisor", "", "")
	if err != nil {
		return nil, err
	}
	_ = root
	switch shape {
	case "nested":
		// supervisor -> 2 leads -> 2 peers each, mixed states.
		l1, err := add("lead-1", "", "supervisor")
		if err != nil {
			return nil, err
		}
		l2, err := add("lead-2", "", "supervisor")
		if err != nil {
			return nil, err
		}
		p1, err := add("peer-1a", "", "lead-1")
		if err != nil {
			return nil, err
		}
		p2, err := add("peer-1b", "", "lead-1")
		if err != nil {
			return nil, err
		}
		p3, err := add("peer-2a", "", "lead-2")
		if err != nil {
			return nil, err
		}
		if _, err := add("peer-2b", "", "lead-2"); err != nil { // never run: idle
			return nil, err
		}
		// lead-1: done, no usage (cost n/a).
		if _, err := h.seedRunOn(l1, "", isDone); err != nil {
			return nil, err
		}
		// peer-1a: done, $0.42.
		runID, err := h.seedRunOn(p1, "", isDone)
		if err != nil {
			return nil, err
		}
		if err := h.seedCost(runID, p1.ID, 0.42); err != nil {
			return nil, err
		}
		// peer-1b: running (hang).
		if _, err := h.seedRunOn(p2, "hang", isRunning); err != nil {
			return nil, err
		}
		// peer-2a: done, $0.17. lead-2 and peer-2b have no run.
		if runID, err = h.seedRunOn(p3, "", isDone); err != nil {
			return nil, err
		}
		if err := h.seedCost(runID, p3.ID, 0.17); err != nil {
			return nil, err
		}
		_ = l2
	case "permission":
		c, err := add("coder-waiting", "permission", "supervisor")
		if err != nil {
			return nil, err
		}
		if _, err := h.startSeedRun(c.ID); err != nil {
			return nil, err
		}
		if err := h.waitTaskState(c.ID, tasktree.StateWaitingPermission); err != nil {
			return nil, err
		}
	case "ratelimited":
		// "rate-limited 47": GLM quota failure, reset ~47 minutes from now.
		c, err := add("coder-limited", "rate-limited 47", "supervisor")
		if err != nil {
			return nil, err
		}
		runID, err := h.startSeedRun(c.ID)
		if err != nil {
			return nil, err
		}
		if err := h.waitSeedRun(runID, isError); err != nil {
			return nil, err
		}
	case "failed":
		c, err := add("coder-failed", "plain-error", "supervisor")
		if err != nil {
			return nil, err
		}
		runID, err := h.startSeedRun(c.ID)
		if err != nil {
			return nil, err
		}
		if err := h.waitSeedRun(runID, isError); err != nil {
			return nil, err
		}
	case "big":
		return h.seedBig(root, nodes, reply)
	default:
		return nil, fmt.Errorf("bad shape %q (want nested|permission|ratelimited|failed|big)", shape)
	}
	return reply(nil), nil
}

// seedBig adds bigChildren live children by store-level inserts (no worktree:
// task.tree only reads store rows), so the 100-node mobile cap shows "n more".
// Only the root has a real worktree, so its task screen opens.
func (h *harness) seedBig(root store.Task, nodes map[string]int64, reply func(map[string]any) map[string]any) (map[string]any, error) {
	for i := 1; i <= bigChildren; i++ {
		t, err := h.store.CreateTask(store.Task{WorkspaceID: root.WorkspaceID, ParentTaskID: &root.ID, Title: fmt.Sprintf("child-%03d", i), Status: "created"})
		if err != nil {
			return nil, err
		}
		if i == 1 {
			nodes["firstChild"] = t.ID
		}
		nodes["lastChild"] = t.ID
	}
	return reply(map[string]any{"children": bigChildren}), nil
}

// waitTaskState polls the registry's run facts until the task shows state.
func (h *harness) waitTaskState(taskID int64, want tasktree.State) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		if f, ok := h.api.Runs.TaskRunFacts()[taskID]; ok && f.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for task %d to be %s", taskID, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// peerRPC sends one RPC as a connected peer and answers its raw result.
func (h *harness) peerRPC(args []string) (map[string]any, error) {
	if err := need(args, 2, "peer-rpc <label> <method> [json params]"); err != nil {
		return nil, err
	}
	p := h.peers[args[0]]
	if p == nil {
		return nil, fmt.Errorf("no peer %q", args[0])
	}
	var params any
	if len(args) > 2 {
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(strings.Join(args[2:], " ")), &raw); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		params = raw
	}
	r, err := p.request(args[1], params, callTimeout)
	if err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message)
	}
	return map[string]any{"result": json.RawMessage(r.Result)}, nil
}
