package wsapi

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
)

// failureWire runs one prompt on the fake GLM agent's scenario and returns
// the raw run.logs result and the terminal run.status payload (as JSON).
func failureWire(t *testing.T, scenario string) (logs map[string]any, event map[string]any) {
	t.Helper()
	return failureWireProfile(t, scenario, nil)
}

// failureWireProfile is failureWire with a run launched under the profile
// that mkProfile creates in the store (nil = no profile).
func failureWireProfile(t *testing.T, scenario string, mkProfile func(db *store.Store) int64) (logs map[string]any, event map[string]any) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, scenario)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ec := newEventConn(t, dialWS(t, srv, "tok"))
	ec.subscribe("sub", TopicRunStatus)

	params := map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"}
	if mkProfile != nil {
		params["profileId"] = mkProfile(db)
	}
	sendRequest(t, ec.ws, "start", "run.start", params)
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
	if logs["status"] != "error" || logs["failureKind"] != "plan_window_exhausted" || logs["window"] != "5h" {
		t.Fatalf("run.logs = %v", logs)
	}
	// The fake's text is Z.ai's zone-less wall clock: the reset is inferred.
	if logs["resetInferred"] != true {
		t.Fatalf("run.logs resetInferred = %v, want true", logs["resetInferred"])
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
		for _, k := range []string{"failureKind", "window", "resetAt", "resetInferred"} {
			if _, ok := m[k]; ok {
				t.Errorf("key %q present in %v", k, m)
			}
		}
	}
}

func TestRunStatusEvent_CarriesFailureFields(t *testing.T) {
	t.Parallel()
	_, event := failureWire(t, "quota-error")
	if event["status"] != "error" || event["failureKind"] != "plan_window_exhausted" || event["window"] != "5h" {
		t.Fatalf("run.status = %v", event)
	}
	if event["resetInferred"] != true {
		t.Fatalf("run.status resetInferred = %v, want true", event["resetInferred"])
	}
}

// TestRunFinishedEvent_CarriesAction: the terminal run.status event carries
// the registry-derived next action alongside the failure facts.
func TestRunFinishedEvent_CarriesAction(t *testing.T) {
	t.Parallel()
	_, event := failureWire(t, "quota-error")
	action, ok := event["action"].(map[string]any)
	if !ok {
		t.Fatalf("run.status action = %v, want an object", event["action"])
	}
	if action["kind"] != failure.ActionRetryNow {
		t.Fatalf("run.status action.kind = %v, want %q (the fake's reset is past)", action["kind"], failure.ActionRetryNow)
	}
	// A run that did not fail carries no action at all.
	_, event = failureWire(t, "stop end_turn")
	if _, ok := event["action"]; ok {
		t.Fatalf("done run carries action: %v", event["action"])
	}
}

// TestToTreeFacts_CarriesResetInferred: the task.tree run info carries the
// marker only when the registry fact does.
func TestToTreeFacts_CarriesResetInferred(t *testing.T) {
	t.Parallel()
	reset := time.Date(2026, 10, 22, 9, 35, 18, 0, time.UTC)
	got := toTreeFacts(map[int64]runs.TaskRunFact{
		1: {FailureKind: "rate_limited", FailureResetAt: &reset, FailureResetInferred: true},
		2: {FailureKind: "rate_limited", FailureResetAt: &reset},
	})
	if !got[1].ResetInferred || got[1].ResetAt == "" {
		t.Fatalf("inferred fact = %+v", got[1])
	}
	if got[2].ResetInferred {
		t.Fatalf("stated fact marked inferred: %+v", got[2])
	}
}

// profileWithFallback creates a glm profile whose author named a codex
// fallback and returns its id.
func profileWithFallback(db *store.Store) int64 {
	fb, err := db.CreateAgentProfile(store.AgentProfile{Name: "fb", Provider: "codex-native"})
	if err != nil {
		panic(err)
	}
	p, err := db.CreateAgentProfile(store.AgentProfile{Name: "main", Provider: "glm", OnRateLimit: "none", FallbackProfileID: &fb.ID})
	if err != nil {
		panic(err)
	}
	return p.ID
}

func TestRunLogs_FallbackProfileIdForRateLimitedRun(t *testing.T) {
	t.Parallel()
	logs, event := failureWireProfile(t, "quota-error", profileWithFallback)
	if logs["failureKind"] != "plan_window_exhausted" {
		t.Fatalf("run.logs = %v", logs)
	}
	fb, ok := logs["fallbackProfileId"].(float64)
	if !ok || fb == 0 {
		t.Fatalf("run.logs fallbackProfileId = %v, want the fallback profile id", logs["fallbackProfileId"])
	}
	if logs["profileId"] == nil || event["profileId"] != logs["profileId"] {
		t.Fatalf("profileId logs = %v, event = %v", logs["profileId"], event["profileId"])
	}
	act, _ := logs["action"].(map[string]any)
	if act["kind"] != failure.ActionSwitchProfile || act["target"] != fmt.Sprintf("profile:%d", int64(fb)) {
		t.Fatalf("run.logs action = %v, want switch_profile to the fallback", logs["action"])
	}
	if _, ok := event["fallbackProfileId"]; ok {
		t.Errorf("run.status event must not carry fallbackProfileId: %v", event)
	}
}

func TestRunLogs_NoFallbackWhenNotRateLimited(t *testing.T) {
	t.Parallel()
	logs, _ := failureWireProfile(t, "stop end_turn", profileWithFallback)
	if _, ok := logs["fallbackProfileId"]; ok {
		t.Fatalf("done run carries fallbackProfileId: %v", logs)
	}
	if logs["profileId"] == nil {
		t.Fatalf("done run lost profileId: %v", logs)
	}
	// And a rate-limited run without a profile has no suggestion either.
	logs, _ = failureWire(t, "quota-error")
	if _, ok := logs["fallbackProfileId"]; ok {
		t.Fatalf("profile-less run carries fallbackProfileId: %v", logs)
	}
}

// TestRunLogs_ActionAuthExpiredIsSignIn: withFallbackAction leaves a human-fix
// action alone even when the profile names a fallback (a fallback never
// covers a broken login).
func TestRunLogs_ActionAuthExpiredIsSignIn(t *testing.T) {
	t.Parallel()
	status := runs.RunStatus{Provider: "glm", FailureKind: failure.KindAuthExpired,
		FailureAction: failure.NextAction(failure.KindAuthExpired, failure.ActionContext{Provider: "glm"})}
	got := withFallbackAction(status, 9)
	if got == nil || got.Kind != failure.ActionSignIn || got.Target != "settings:providers?provider=glm" {
		t.Fatalf("action = %+v, want sign_in to the provider's settings", got)
	}
	if withFallbackAction(runs.RunStatus{}, 9) != nil {
		t.Fatal("a run without a failure has no action")
	}
}

// TestInboxList_RunFailedFromLatestRun: a task whose latest run failed on a
// plan window appears in inbox.list as a run_failed item carrying the action;
// a clean task has none.
func TestInboxList_RunFailedFromLatestRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scenario string
		want     bool
	}{{"quota-error", true}, {"stop end_turn", false}} {
		wm, db := newTestWorkspaceManager(t)
		task := newTestTask(t, wm, tc.scenario)
		srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
		ec := newEventConn(t, dialWS(t, srv, "tok"))
		ec.subscribe("sub", TopicRunStatus)
		sendRequest(t, ec.ws, "start", "run.start", map[string]any{"taskId": task.ID, "provider": "glm", "prompt": "hi"})
		resp := ec.nextResponse("start", 15*time.Second)
		var started runStartResult
		if err := json.Unmarshal(resp.Result, &started); err != nil {
			t.Fatal(err)
		}
		for {
			ev := ec.nextEvent(15 * time.Second)
			var p runStatusPayload
			decodePayload(t, ev, &p)
			if p.RunID == started.RunID && p.Status != "running" {
				break
			}
		}
		sendRequest(t, ec.ws, "inbox", "inbox.list", map[string]any{})
		resp = ec.nextResponse("inbox", 5*time.Second)
		var out struct {
			Items []struct {
				Key    string `json:"key"`
				Kind   string `json:"kind"`
				Action *struct {
					Kind string `json:"kind"`
				} `json:"action"`
			} `json:"items"`
		}
		if err := json.Unmarshal(resp.Result, &out); err != nil {
			t.Fatalf("%s: %v (%s)", tc.scenario, err, resp.Result)
		}
		found := false
		for _, it := range out.Items {
			if it.Kind == "run_failed" {
				found = true
				if it.Key != "runfail:"+started.RunID || it.Action == nil || it.Action.Kind == "" {
					t.Fatalf("%s: item = %+v", tc.scenario, it)
				}
			}
		}
		if found != tc.want {
			t.Fatalf("%s: run_failed present = %v, want %v (items %+v)", tc.scenario, found, tc.want, out.Items)
		}
	}
}
