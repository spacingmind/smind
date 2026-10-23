package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
)

// failureWire runs one prompt on the fake GLM agent's scenario and returns
// the raw run.logs result and the terminal run.status payload (as JSON).
func failureWire(t *testing.T, scenario string) (logs map[string]any, event map[string]any) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
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
	for {
		ev := ec.nextEvent(15 * time.Second)
		var p runStatusPayload
		decodePayload(t, ev, &p)
		if p.RunID == started.RunID && p.Status != "running" {
			b, _ := json.Marshal(p)
			event = map[string]any{}
			if err := json.Unmarshal(b, &event); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	sendRequest(t, ec.ws, "logs", "run.logs", map[string]any{"runId": started.RunID})
	resp = ec.nextResponse("logs", 5*time.Second)
	logs = map[string]any{}
	if err := json.Unmarshal(resp.Result, &logs); err != nil {
		t.Fatal(err)
	}
	return logs, event
}

func TestRunLogs_CarriesFailureFields(t *testing.T) {
	t.Parallel()
	logs, _ := failureWire(t, "quota-error")
	if logs["status"] != "error" || logs["failureKind"] != "rate_limited" || logs["window"] != "5h" {
		t.Fatalf("run.logs = %v", logs)
	}
	// resetAt is RFC 3339 UTC when known (the fake's text carries a full date,
	// which the classifier deliberately does not guess a time from).
	if s, ok := logs["resetAt"].(string); ok {
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			t.Fatalf("resetAt %q: %v", s, err)
		}
	}
	// Formatting of a known reset.
	x := time.Unix(1790000000, 0)
	got := runs.FailureResetRFC3339(runs.RunStatus{FailureResetAt: &x})
	if want := x.UTC().Format(time.RFC3339); got != want || got[len(got)-1] != 'Z' {
		t.Fatalf("FailureResetRFC3339 = %q, want %q", got, want)
	}
	b, _ := json.Marshal(runLogsResult{FailureKind: "rate_limited", Window: "5h", ResetAt: got})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["resetAt"] != got {
		t.Fatalf("marshalled = %s", b)
	}
}

func TestRunLogs_FailureFieldsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	logs, event := failureWire(t, "stop end_turn")
	for _, m := range []map[string]any{logs, event} {
		for _, k := range []string{"failureKind", "window", "resetAt"} {
			if _, ok := m[k]; ok {
				t.Errorf("key %q present in %v", k, m)
			}
		}
	}
}

func TestRunStatusEvent_CarriesFailureFields(t *testing.T) {
	t.Parallel()
	_, event := failureWire(t, "quota-error")
	if event["status"] != "error" || event["failureKind"] != "rate_limited" || event["window"] != "5h" {
		t.Fatalf("run.status = %v", event)
	}
}
