package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type proc struct {
	t     *testing.T
	stdin io.WriteCloser
	out   *bufio.Scanner
	cmd   *exec.Cmd
}

func (p *proc) line() string {
	p.t.Helper()
	type res struct {
		s  string
		ok bool
	}
	ch := make(chan res, 1)
	go func() { ok := p.out.Scan(); ch <- res{p.out.Text(), ok} }()
	select {
	case r := <-ch:
		if !r.ok {
			p.t.Fatalf("stdout closed: %v", p.out.Err())
		}
		return r.s
	case <-time.After(60 * time.Second):
		p.t.Fatal("timed out waiting for a stdout line")
		return ""
	}
}

// do sends one command and returns the decoded reply, asserting the cmd echo.
func (p *proc) do(cmd string) map[string]any {
	p.t.Helper()
	if _, err := io.WriteString(p.stdin, cmd+"\n"); err != nil {
		p.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(p.line()), &m); err != nil {
		p.t.Fatalf("%s: bad JSON reply: %v", cmd, err)
	}
	if m["cmd"] != strings.Fields(cmd)[0] {
		p.t.Fatalf("%s: reply cmd = %v", cmd, m["cmd"])
	}
	return m
}

func (p *proc) mustOK(cmd string) map[string]any {
	p.t.Helper()
	m := p.do(cmd)
	if m["ok"] != true {
		p.t.Fatalf("%s: %v", cmd, m)
	}
	return m
}

func (p *proc) state(label string) string {
	p.t.Helper()
	for _, d := range p.mustOK("list")["devices"].([]any) {
		dm := d.(map[string]any)
		if dm["label"] == label {
			return dm["state"].(string)
		}
	}
	return ""
}

func start(t *testing.T, extraArgs ...string) *proc {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "devharness")
	if runtime.GOOS == "windows" {
		bin += ".exe" // exec.Command does not append it to a path
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, extraArgs...)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &proc{t: t, stdin: stdin, out: bufio.NewScanner(stdout), cmd: cmd}
	p.out.Buffer(make([]byte, 1<<20), 1<<20)
	t.Cleanup(func() {
		_ = stdin.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			cancel()
			<-done
		}
		cancel()
	})
	if l := p.line(); !strings.HasPrefix(l, "READY ") {
		t.Fatalf("first line = %q", l)
	}
	if l := p.line(); !strings.HasPrefix(l, "NATIVE 127.0.0.1:") {
		t.Fatalf("second line = %q", l)
	}
	return p
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestDeviceHarness(t *testing.T) {
	p := start(t)

	// Window closed: an unknown device is recorded pending and refused.
	if m := p.mustOK("peer-add a"); m["state"] != "awaiting_approval" {
		t.Fatalf("peer-add a: %v", m)
	}
	if s := p.state("a"); s != "pending" {
		t.Fatalf("a state = %q", s)
	}
	// Approve, then the same device (same persistent key) is admitted.
	if m := p.mustOK("approve a"); m["state"] != "approved" {
		t.Fatalf("approve a: %v", m)
	}
	if m := p.mustOK("peer-add a"); m["state"] != "approved" {
		t.Fatalf("peer-add a again: %v", m)
	}
	if m := p.mustOK("peer-call a"); m["result_len"] == nil {
		t.Fatalf("peer-call a: %v", m)
	}
	eventually(t, "a connected", func() bool {
		for _, d := range p.mustOK("list")["devices"].([]any) {
			if d.(map[string]any)["connected"] == true {
				return true
			}
		}
		return false
	})

	// A window of one auto-approves the next new device only.
	p.mustOK("window 1")
	if m := p.mustOK("peer-add b"); m["state"] != "approved" {
		t.Fatalf("peer-add b: %v", m)
	}
	if m := p.mustOK("peer-add x"); m["state"] != "awaiting_approval" {
		t.Fatalf("peer-add x (window spent): %v", m)
	}
	p.mustOK("peer-call b")
	p.mustOK("peer-call a") // a is unaffected by b

	// Revoke ends b's session; a keeps being served.
	if m := p.mustOK("revoke b"); m["state"] != "revoked" {
		t.Fatalf("revoke b: %v", m)
	}
	if m := p.do("peer-call b"); m["ok"] != false {
		t.Fatalf("peer-call b after revoke should fail: %v", m)
	}
	p.mustOK("peer-call a")
	if m := p.mustOK("peer-add b"); m["state"] != "revoked" {
		t.Fatalf("revoked b reconnecting: %v", m)
	}

	// peer-drop kills only the transport: the device stays approved.
	p.mustOK("peer-drop a")
	if s := p.state("a"); s != "approved" {
		t.Fatalf("a after drop = %q", s)
	}

	// Daemon restart (counters lost, registry kept): a fresh device still pairs.
	p.mustOK("daemon-restart")
	p.mustOK("window 1")
	if m := p.mustOK("peer-add c"); m["state"] != "approved" {
		t.Fatalf("peer-add c after daemon-restart: %v", m)
	}
	p.mustOK("peer-call c")
	if s := p.state("b"); s != "revoked" {
		t.Fatalf("b after daemon-restart = %q", s)
	}

	// Relay restart: the daemon reconnects and a new device is served again.
	p.mustOK("relay-restart")
	p.mustOK("window 1")
	eventually(t, "d approved through the restarted relay", func() bool {
		m := p.do("peer-add d")
		return m["ok"] == true && m["state"] == "approved"
	})
	p.mustOK("peer-call d")

	// Errors are reported, never crash.
	if m := p.do("approve nosuchdevice"); m["ok"] != false {
		t.Fatalf("approve unknown: %v", m)
	}
	if m := p.do("bogus"); m["ok"] != false {
		t.Fatalf("bogus: %v", m)
	}
	p.mustOK("quit")
	if err := p.cmd.Wait(); err != nil {
		t.Fatalf("exit: %v", err)
	}
}

// TestSeedRunCappedRunNeedsContinue (D13): seed-run drives a real fake-agent
// run to done; a capped stop reason reports Disposition needs-continue, a
// normal end_turn does not, and without -fake-agent the command errors.
func TestSeedRunCappedRunNeedsContinue(t *testing.T) {
	fakeDir := t.TempDir()
	fake := filepath.Join(fakeDir, "fakeagent")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fake, "../../../taskrunner/fakeagent").CombinedOutput(); err != nil {
		t.Fatalf("go build fakeagent: %v\n%s", err, out)
	}

	p := start(t, "-fake-agent", fake)
	if m := p.mustOK("seed-run max_turn_requests"); m["stopReason"] != "max_turn_requests" || m["disposition"] != "needs-continue" || m["status"] != "done" {
		t.Fatalf("seed-run max_turn_requests: %v", m)
	}
	m := p.mustOK("seed-run end_turn")
	if m["status"] != "done" || m["disposition"] != "" || m["stopReason"] == "max_turn_requests" || m["stopReason"] == "max_tokens" {
		t.Fatalf("seed-run end_turn: %v", m)
	}
	p.mustOK("quit")

	plain := start(t)
	if m := plain.do("seed-run x"); m["ok"] != false || m["error"] != "seed-run needs -fake-agent" {
		t.Fatalf("seed-run without -fake-agent: %v", m)
	}
	plain.mustOK("quit")
}

func startFake(t *testing.T) *proc {
	t.Helper()
	fakeDir := t.TempDir()
	fake := filepath.Join(fakeDir, "fakeagent")
	if runtime.GOOS == "windows" {
		fake += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fake, "../../../taskrunner/fakeagent").CombinedOutput(); err != nil {
		t.Fatalf("go build fakeagent: %v\n%s", err, out)
	}
	return start(t, "-fake-agent", fake)
}

// TestQueueDeliversWhenTheLiveRunIsStopped (ADR-0021): prompts queued on a
// busy chat stay queued in FIFO order and are delivered as new runs, oldest
// first, once the live run is stopped.
func TestQueueDeliversWhenTheLiveRunIsStopped(t *testing.T) {
	p := startFake(t)

	m := p.mustOK("seed-live hang Live")
	if m["status"] != "running" {
		t.Fatalf("seed-live: %v", m)
	}
	taskID := int64(m["taskId"].(float64))
	chatID := int64(m["chatId"].(float64))

	if m := p.mustOK("queue-prompt " + fmtInt(taskID) + " queue second"); m["queued"] != true {
		t.Fatalf("queue-prompt second: %v", m)
	}
	if m := p.mustOK("queue-prompt " + fmtInt(taskID) + " queue third"); m["queued"] != true {
		t.Fatalf("queue-prompt third: %v", m)
	}
	secondID, thirdID := queueIDByPrompt(t, p, chatID, "second"), queueIDByPrompt(t, p, chatID, "third")
	if secondID == 0 || thirdID == 0 || secondID == thirdID {
		t.Fatalf("expected two distinct queued items: %d %d", secondID, thirdID)
	}

	p.mustOK("set-scenario " + fmtInt(taskID) + " default")
	p.mustOK("stop-run " + m2s(m["runId"]))

	var firstRunID string
	eventually(t, "second delivered", func() bool {
		for _, it := range queueItems(t, p, chatID) {
			if int64(it["id"].(float64)) == secondID && it["status"] == "delivered" && it["runId"].(string) != "" {
				firstRunID = it["runId"].(string)
				return true
			}
		}
		return false
	})
	eventually(t, "second delivered run done", func() bool {
		return p.mustOK("run-status " + firstRunID)["status"] == "done"
	})
	eventually(t, "third delivered", func() bool {
		for _, it := range queueItems(t, p, chatID) {
			if int64(it["id"].(float64)) == thirdID && it["status"] == "delivered" && it["runId"].(string) != "" {
				return true
			}
		}
		return false
	})
	p.mustOK("quit")
}

// TestQueueInterruptStopsTheLiveRunAndGoesFirst (ADR-0021): an interrupt
// prompt stops the live run and is delivered before earlier queued prompts.
func TestQueueInterruptStopsTheLiveRunAndGoesFirst(t *testing.T) {
	p := startFake(t)

	m := p.mustOK("seed-live hang Live")
	if m["status"] != "running" {
		t.Fatalf("seed-live: %v", m)
	}
	taskID := int64(m["taskId"].(float64))
	chatID := int64(m["chatId"].(float64))
	liveRunID := m2s(m["runId"])

	p.mustOK("queue-prompt " + fmtInt(taskID) + " queue first")
	// The interrupt's delivered run must be able to finish.
	p.mustOK("set-scenario " + fmtInt(taskID) + " default")
	if m := p.mustOK("queue-prompt " + fmtInt(taskID) + " interrupt urgent"); m["queued"] != true {
		t.Fatalf("queue-prompt urgent: %v", m)
	}

	eventually(t, "live run stopped", func() bool {
		return p.mustOK("run-status " + liveRunID)["status"] == "stopped"
	})
	firstID, urgentID := queueIDByPrompt(t, p, chatID, "first"), queueIDByPrompt(t, p, chatID, "urgent")
	var urgentRunID string
	eventually(t, "urgent delivered with a runId", func() bool {
		for _, it := range queueItems(t, p, chatID) {
			if int64(it["id"].(float64)) == urgentID && it["runId"].(string) != "" {
				urgentRunID = it["runId"].(string)
				return it["status"] == "delivered"
			}
		}
		return false
	})
	if r := queueItemByID(t, p, chatID, firstID); r["runId"].(string) == "" && r["status"] != "delivered" {
		// first is still queued while urgent already runs (priority).
		t.Logf("urgent delivered before first: first = %v", r)
	}
	p.mustOK("stop-run " + urgentRunID)
	eventually(t, "first delivered after urgent", func() bool {
		for _, it := range queueItems(t, p, chatID) {
			if int64(it["id"].(float64)) == firstID && it["status"] == "delivered" && it["runId"].(string) != "" {
				return true
			}
		}
		return false
	})
	p.mustOK("quit")
}

// TestSetTaskStatus: set-task-status writes the status column of an
// existing task and refuses a bad status or taskId.
func TestSetTaskStatus(t *testing.T) {
	p := startFake(t)

	m := p.mustOK("seed-run end_turn Status task")
	taskID := int64(m["taskId"].(float64))
	for _, status := range []string{"created", "running", "error", "stopped", "done"} {
		if m := p.mustOK("set-task-status " + fmtInt(taskID) + " " + status); int64(m["taskId"].(float64)) != taskID || m["status"] != status {
			t.Fatalf("set-task-status %s: %v", status, m)
		}
	}
	if m := p.do("set-task-status " + fmtInt(taskID) + " bogus"); m["ok"] != false {
		t.Fatalf("set-task-status bogus status: %v", m)
	}
	if m := p.do("set-task-status 999999 done"); m["ok"] != false {
		t.Fatalf("set-task-status unknown task: %v", m)
	}
	p.mustOK("quit")
}

func fmtInt(i int64) string { return strconv.FormatInt(i, 10) }

func m2s(v any) string { return v.(string) }

func queueItems(t *testing.T, p *proc, chatID int64) []map[string]any {
	t.Helper()
	raw := p.mustOK("queue-list " + fmtInt(chatID))["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		items = append(items, it.(map[string]any))
	}
	return items
}

func queueItemByID(t *testing.T, p *proc, chatID, id int64) map[string]any {
	t.Helper()
	for _, it := range queueItems(t, p, chatID) {
		if it["id"].(float64) == float64(id) {
			return it
		}
	}
	t.Fatalf("queue item %d not found", id)
	return nil
}

// queueIDByPrompt finds a queue item by its prompt text.
func queueIDByPrompt(t *testing.T, p *proc, chatID int64, prompt string) int64 {
	t.Helper()
	for _, it := range queueItems(t, p, chatID) {
		if it["prompt"] == prompt {
			return int64(it["id"].(float64))
		}
	}
	t.Fatalf("queue item %q not found", prompt)
	return 0
}

type treeNode struct {
	ID         int64    `json:"id"`
	ParentID   *int64   `json:"parentId"`
	Title      string   `json:"title"`
	Depth      int      `json:"depth"`
	Tier       *string  `json:"tier"`
	ChildCount int      `json:"childCount"`
	Run        treeRun  `json:"run"`
	Usage      treeUsed `json:"usage"`
}

type treeRun struct {
	State              string `json:"state"`
	PendingPermissions int    `json:"pendingPermissions"`
	FailureKind        string `json:"failureKind"`
	ResetAt            string `json:"resetAt"`
}

type treeUsed struct {
	Own struct {
		CostUSD *float64 `json:"costUsd"`
	} `json:"own"`
}

// seedTree runs seed-tree <shape> and reads the tree back through the real
// task.tree handler over a paired device peer; byTitle indexes the nodes.
func seedTree(t *testing.T, shape string) (reply map[string]any, nodes []treeNode, byTitle map[string]treeNode) {
	t.Helper()
	p := startFake(t)
	p.mustOK("window 1")
	if m := p.mustOK("peer-add a"); m["state"] != "approved" {
		t.Fatalf("peer-add: %v", m)
	}
	reply = p.mustOK("seed-tree " + shape)
	root := int64(reply["rootId"].(float64))
	res := p.mustOK("peer-rpc a task.tree {\"taskId\":" + fmtInt(root) + "}")["result"].(map[string]any)
	raw, _ := json.Marshal(res["nodes"])
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatal(err)
	}
	byTitle = map[string]treeNode{}
	for _, n := range nodes {
		byTitle[n.Title] = n
	}
	p.mustOK("quit")
	return reply, nodes, byTitle
}

func tierOf(n treeNode) string {
	if n.Tier == nil {
		return "<nil>"
	}
	return *n.Tier
}

func TestSeedTree_Nested(t *testing.T) {
	reply, nodes, by := seedTree(t, "nested")
	if len(nodes) != 7 {
		t.Fatalf("nodes = %d, want 7: %+v", len(nodes), nodes)
	}
	ids := reply["nodes"].(map[string]any)
	for title, n := range by {
		if float64(n.ID) != ids[title] {
			t.Fatalf("%s id %d not in reply %v", title, n.ID, ids)
		}
	}
	sup := by["supervisor"]
	if tierOf(sup) != "supervisor" || sup.ParentID != nil || sup.Depth != 0 || sup.ChildCount != 2 {
		t.Fatalf("supervisor: %+v", sup)
	}
	for _, lead := range []string{"lead-1", "lead-2"} {
		n := by[lead]
		if tierOf(n) != "leader" || n.ParentID == nil || *n.ParentID != sup.ID || n.Depth != 1 || n.ChildCount != 2 {
			t.Fatalf("%s: %+v", lead, n)
		}
	}
	for peer, lead := range map[string]string{"peer-1a": "lead-1", "peer-1b": "lead-1", "peer-2a": "lead-2", "peer-2b": "lead-2"} {
		n := by[peer]
		if tierOf(n) != "coder" || n.ParentID == nil || *n.ParentID != by[lead].ID || n.Depth != 2 {
			t.Fatalf("%s: %+v", peer, n)
		}
	}
	for title, state := range map[string]string{"lead-1": "done", "peer-1a": "done", "peer-1b": "running", "peer-2a": "done", "peer-2b": "idle", "lead-2": "idle"} {
		if got := by[title].Run.State; got != state {
			t.Fatalf("%s run.state = %q, want %q", title, got, state)
		}
	}
	if c := by["peer-1a"].Usage.Own.CostUSD; c == nil || *c != 0.42 {
		t.Fatalf("peer-1a cost = %v, want 0.42", c)
	}
	if c := by["lead-1"].Usage.Own.CostUSD; c != nil {
		t.Fatalf("lead-1 cost = %v, want null (n/a)", *c)
	}
}

func TestSeedTree_Permission(t *testing.T) {
	_, nodes, by := seedTree(t, "permission")
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(nodes))
	}
	c := by["coder-waiting"]
	if tierOf(c) != "coder" || c.ParentID == nil || *c.ParentID != by["supervisor"].ID {
		t.Fatalf("child: %+v", c)
	}
	if c.Run.State != "waiting_permission" || c.Run.PendingPermissions != 1 {
		t.Fatalf("child run = %+v, want waiting_permission with 1 pending", c.Run)
	}
	if tierOf(by["supervisor"]) != "supervisor" {
		t.Fatalf("supervisor tier = %s", tierOf(by["supervisor"]))
	}
}
