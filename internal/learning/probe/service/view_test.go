package probesvc

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/probe/contrast"
	"github.com/spacingmind/smind/internal/store"
)

func keysOf(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return keysOfRaw(t, b)
}

func keysOfRaw(t *testing.T, b []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not an object: %s", b)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func wantKeys(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s keys\n got  %v\n want %v", what, got, want)
	}
}

// TestProbeView_WireContractKeys pins the producer side of "Probe wire
// contract" in docs/plans/active/learning-probe.md: the exact JSON keys of
// ProbeView, ArmView, ContrastView and MetricRow, and the null / [] rules.
func TestProbeView_WireContractKeys(t *testing.T) {
	t.Parallel()
	yes, a2, noise := true, 2.0, 0.5
	v := View{ID: 1, ProposalID: 2, WorkspaceID: 3, Quick: false, Status: "done", Verdict: ptr("helped"), VerdictReason: "r",
		EstUSD: 1, CostUSD: 1, StartedAt: "2026-10-22T10:00:00Z", FinishedAt: "2026-10-22T10:05:00Z", ElapsedMs: 300000,
		Arms: []ArmView{
			{Role: "A", Status: "done", RunID: "run-a", StartedAt: "2026-10-22T10:00:00Z", ElapsedMs: 1, CostUSD: 0.1, Turns: 1, ToolCalls: 1,
				FailedToolCalls: 0, PermissionDenials: 0, FailureRecurred: nil, ProducedDiff: true, DiffBytes: 10, Diff: "d", FinalMessage: "f",
				Check: &Check{Passed: true, Output: "ok"}},
			{Role: "B", Status: "done", RunID: "run-b", StartedAt: "2026-10-22T10:01:00Z", ElapsedMs: 1, CostUSD: 0.1, LoadedSkill: &yes, FailureRecurred: &yes},
		},
		Contrast: &ContrastView{Loaded: true, NoiseFloor: false, Metrics: []MetricRow{{Key: "turns", A: 1, A2: &a2, B: 2, DeltaAB: 1, Noise: &noise, Worse: true}}}}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	wantKeys(t, "ProbeView", keysOfRaw(t, raw), "id", "proposalId", "workspaceId", "quick", "status", "verdict", "verdictReason",
		"estUSD", "costUSD", "startedAt", "finishedAt", "elapsedMs", "arms", "contrast")

	var arms []json.RawMessage
	if err := json.Unmarshal(top["arms"], &arms); err != nil || len(arms) != 2 {
		t.Fatalf("arms = %s: %v", top["arms"], err)
	}
	wantKeys(t, "ArmView A", keysOfRaw(t, arms[0]), "role", "status", "runId", "startedAt", "elapsedMs", "costUSD", "turns", "toolCalls",
		"failedToolCalls", "permissionDenials", "failureRecurred", "producedDiff", "diffBytes", "diff", "finalMessage", "check")
	wantKeys(t, "ArmView B", keysOfRaw(t, arms[1]), "role", "status", "runId", "startedAt", "elapsedMs", "costUSD", "turns", "toolCalls",
		"failedToolCalls", "permissionDenials", "loadedSkill", "failureRecurred", "producedDiff", "diffBytes")
	var a0 map[string]json.RawMessage
	_ = json.Unmarshal(arms[0], &a0)
	if string(a0["failureRecurred"]) != "null" {
		t.Errorf("failureRecurred without a signature = %s, want null", a0["failureRecurred"])
	}
	wantKeys(t, "Check", keysOfRaw(t, a0["check"]), "passed", "output")

	var con map[string]json.RawMessage
	_ = json.Unmarshal(top["contrast"], &con)
	wantKeys(t, "ContrastView", keysOfRaw(t, top["contrast"]), "loaded", "noiseFloor", "metrics")
	var rows []json.RawMessage
	_ = json.Unmarshal(con["metrics"], &rows)
	wantKeys(t, "MetricRow", keysOfRaw(t, rows[0]), "key", "a", "a2", "b", "deltaAB", "noise", "worse")
	// a2 and noise are null, not absent, when there is no replicate.
	bare := keysOf(t, MetricRow{Key: "turns"})
	wantKeys(t, "MetricRow (no replicate)", bare, "key", "a", "a2", "b", "deltaAB", "noise", "worse")
	if b, _ := json.Marshal(MetricRow{Key: "turns"}); !strings.Contains(string(b), `"a2":null`) || !strings.Contains(string(b), `"noise":null`) {
		t.Errorf("MetricRow without a replicate = %s, want null a2 and noise", b)
	}
}

func ptr[T any](v T) *T { return &v }

// The null-versus-[] rules hold on what the service really emits.
func TestProbeView_WireContractNullsAndEmpties(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)

	// Estimate reasons are [] when eligible (before any probe row exists).
	est, err := p.svc.Estimate(context.Background(), p.prop.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	eb, _ := json.Marshal(est)
	var em map[string]json.RawMessage
	_ = json.Unmarshal(eb, &em)
	if !est.Eligible || string(em["reasons"]) != "[]" {
		t.Errorf("eligible=%v reasons = %s, want an eligible estimate with []", est.Eligible, em["reasons"])
	}
	// An ineligible estimate carries reasons (code + message), never null.
	if err := p.st.SetWorkspaceLearningMode(p.ws.ID, "off"); err != nil {
		t.Fatal(err)
	}
	est, _ = p.svc.Estimate(context.Background(), p.prop.ID, false)
	if len(est.Reasons) == 0 {
		t.Error("ineligible estimate has no reasons")
	}
	wantKeys(t, "Reason", keysOf(t, est.Reasons[0]), "code", "message")
	if err := p.st.SetWorkspaceLearningMode(p.ws.ID, "propose"); err != nil {
		t.Fatal(err)
	}

	// A queued probe: arms present (pending), verdict null, no contrast, no finishedAt.
	pr, err := p.st.CreateProbe(store.LearningProbe{WorkspaceID: p.ws.ID, ProposalID: p.prop.ID, Status: "queued", Contrast: "{}", StartedAt: p.svc.d.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.st.UpsertProbeArm(store.LearningProbeArm{ProbeID: pr.ID, Role: "A", Seq: 0, Status: "pending", Metrics: "{}"}); err != nil {
		t.Fatal(err)
	}
	v, ok, err := p.svc.Get(p.prop.ID)
	if err != nil || !ok {
		t.Fatalf("Get: %v %v", ok, err)
	}
	raw, _ := json.Marshal(v)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	wantKeys(t, "queued ProbeView", keysOfRaw(t, raw), "id", "proposalId", "workspaceId", "quick", "status", "verdict", "verdictReason",
		"estUSD", "costUSD", "startedAt", "elapsedMs", "arms")
	if string(top["verdict"]) != "null" {
		t.Errorf("verdict before a decision = %s, want null", top["verdict"])
	}
	var armRows []map[string]json.RawMessage
	_ = json.Unmarshal(top["arms"], &armRows)
	if len(armRows) != 1 || string(armRows[0]["failureRecurred"]) != "null" {
		t.Errorf("arms = %s", top["arms"])
	}

	// A probe row with no arms still serialises arms as [].
	pr2, err := p.st.CreateProbe(store.LearningProbe{WorkspaceID: p.ws.ID, ProposalID: p.prop.ID, Status: "failed", Contrast: "{}", StartedAt: p.svc.d.Now()})
	if err != nil {
		t.Fatal(err)
	}
	list, err := p.svc.List(p.ws.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, lv := range list {
		if lv.ID == pr2.ID {
			b, _ := json.Marshal(lv)
			var m map[string]json.RawMessage
			_ = json.Unmarshal(b, &m)
			if string(m["arms"]) != "[]" {
				t.Errorf("arms of a probe without arm rows = %s, want []", m["arms"])
			}
		}
	}

	cb, _ := json.Marshal(toContrastView(contrast.Contrast{}))
	var cm map[string]json.RawMessage
	_ = json.Unmarshal(cb, &cm)
	if string(cm["metrics"]) != "[]" {
		t.Errorf("metrics of an empty contrast = %s, want []", cm["metrics"])
	}
}
