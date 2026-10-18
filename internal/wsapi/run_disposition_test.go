package wsapi

import (
	"encoding/json"
	"testing"
	"time"
)

// dispositionView is what run.list, run.logs and the run.status event said
// about one finished run (orchestration-parity D13).
type dispositionView struct {
	listStatus, listStop, listDisposition string
	logsStatus, logsStop, logsDisposition string
	eventStatus, eventDisposition         string
}

// runDispositionFor runs one prompt whose fake agent ends its turn with
// stopReason and waits for it to finish.
func runDispositionFor(t *testing.T, stopReason string) dispositionView {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "stop "+stopReason)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicRunStatus)

	sendRequest(t, ec.ws, "start", "run.start", map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
	resp := ec.nextResponse("start", 15*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.start: %v", resp.Error.Message)
	}
	var started runStartResult
	if err := json.Unmarshal(resp.Result, &started); err != nil {
		t.Fatal(err)
	}

	var v dispositionView
	for { // the terminal run.status event
		ev := ec.nextEvent(15 * time.Second)
		var p runStatusPayload
		decodePayload(t, ev, &p)
		if p.RunID == started.RunID && p.Status != "running" {
			v.eventStatus, v.eventDisposition = p.Status, p.Disposition
			break
		}
	}

	sendRequest(t, ec.ws, "list", "run.list", map[string]any{"taskId": task.ID})
	resp = ec.nextResponse("list", 5*time.Second)
	var list []struct {
		ID, Status, StopReason, Disposition string
	}
	if err := json.Unmarshal(resp.Result, &list); err != nil || len(list) != 1 {
		t.Fatalf("run.list = %s (%v)", resp.Result, err)
	}
	v.listStatus, v.listStop, v.listDisposition = list[0].Status, list[0].StopReason, list[0].Disposition

	sendRequest(t, ec.ws, "logs", "run.logs", map[string]any{"runId": started.RunID})
	resp = ec.nextResponse("logs", 5*time.Second)
	var raw struct {
		Status      string `json:"status"`
		StopReason  string `json:"stopReason"`
		Disposition string `json:"disposition"`
	}
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		t.Fatal(err)
	}
	v.logsStatus, v.logsStop, v.logsDisposition = raw.Status, raw.StopReason, raw.Disposition
	return v
}

// TestRunDispositionNeedsContinueOnAMaxTurnStop: a run cut off at max_turn_requests
// still ends "done" (no new status), and every surface that reports its stop
// reason also says needs-continue.
func TestRunDispositionNeedsContinueOnAMaxTurnStop(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"max_turn_requests", "max_tokens"} {
		v := runDispositionFor(t, reason)
		if v.listStatus != "done" || v.logsStatus != "done" || v.eventStatus != "done" {
			t.Errorf("%s: statuses = list %q logs %q event %q, want done everywhere (no new status)", reason, v.listStatus, v.logsStatus, v.eventStatus)
		}
		if v.listStop != reason || v.logsStop != reason {
			t.Errorf("%s: stop reasons = list %q logs %q", reason, v.listStop, v.logsStop)
		}
		if v.listDisposition != "needs-continue" || v.logsDisposition != "needs-continue" || v.eventDisposition != "needs-continue" {
			t.Errorf("%s: dispositions = list %q logs %q event %q, want needs-continue", reason, v.listDisposition, v.logsDisposition, v.eventDisposition)
		}
	}
}

func TestRunDispositionAbsentForANormalFinish(t *testing.T) {
	t.Parallel()
	v := runDispositionFor(t, "end_turn")
	if v.listDisposition != "" || v.logsDisposition != "" || v.eventDisposition != "" {
		t.Fatalf("a normal finish has dispositions list %q logs %q event %q, want none", v.listDisposition, v.logsDisposition, v.eventDisposition)
	}
}
