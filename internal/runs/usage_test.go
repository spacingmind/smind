package runs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

func usageEvent(in, out int64, cost float64) Event {
	model := "glm-test"
	return Event{Type: taskrunner.EventTypeUsage, Usage: &taskrunner.Usage{
		Input: &in, Output: &out, CostUSD: &cost, Model: &model,
		Source: taskrunner.UsageSourceClaudeResult,
	}}
}

func TestRunList_CarriesUsageTotals(t *testing.T) {
	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	runner := newTestRunner(wm)

	withUsage, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, withUsage, StatusDone, 5*time.Second)
	r, _ := reg.get(withUsage)
	reg.record(r, usageEvent(100, 20, 0.25))

	// A run that finishes without reaching turn end emits no usage event: a
	// hung turn, stopped.
	hangTask := newTestTask(t, wm, "hang")
	noUsage, err := reg.Start(context.Background(), wm, runner, hangTask.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForHistoryLen(t, reg, noUsage, 1, 5*time.Second)
	if err := reg.Stop(noUsage); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	waitForStatus(t, reg, noUsage, StatusStopped, 5*time.Second)

	// A turn that finished but whose backend reported nothing still emits a
	// usage event: source not_reported, every number absent.
	notReported, err := reg.Start(context.Background(), wm, runner, task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, notReported, StatusDone, 5*time.Second)

	check := func(label string, reg *Registry) {
		t.Helper()
		byID := map[string]RunSummary{}
		for _, s := range reg.List(0) {
			byID[s.ID] = s
		}
		u := byID[withUsage].Usage
		if u == nil || u.InputTokens == nil || *u.InputTokens != 100 || *u.OutputTokens != 20 ||
			u.CostUSD == nil || *u.CostUSD != 0.25 || u.Model == nil || *u.Model != "glm-test" {
			t.Errorf("%s: usage = %+v, want 100/20 cost 0.25 model glm-test", label, u)
		}
		if u != nil && u.CachedInputTokens != nil {
			t.Errorf("%s: CachedInputTokens = %v, want nil (unreported)", label, *u.CachedInputTokens)
		}
		if byID[noUsage].Usage != nil {
			t.Errorf("%s: run without a usage event has Usage %+v, want nil", label, byID[noUsage].Usage)
		}
		b, _ := json.Marshal(byID[noUsage])
		if strings.Contains(string(b), `"usage"`) {
			t.Errorf("%s: no-usage run JSON has a usage key: %s", label, b)
		}
		b, _ = json.Marshal(byID[notReported])
		var wire struct {
			Usage json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(b, &wire); err != nil || string(wire.Usage) != `{"source":"not_reported"}` {
			t.Errorf("%s: not-reported run usage = %s, want {\"source\":\"not_reported\"}", label, wire.Usage)
		}
	}
	check("live", reg)
	// A fresh registry rehydrates finished runs; usage comes from the store.
	check("rehydrated", newTestRegistry(t, st))

	if _, err := st.GetRunUsage(noUsage); err == nil {
		t.Error("run without a usage event got a run_usage row")
	}
	row, err := st.GetRunUsage(notReported)
	if err != nil || row.Source != string(taskrunner.UsageSourceNotReported) || row.InputTokens != nil || row.CostUSD != nil {
		t.Errorf("not-reported run_usage row = %+v, %v; want all-NULL, source not_reported", row, err)
	}
}

func TestUsageEvent_RoundTripsAndOldRowsDecode(t *testing.T) {
	t.Parallel()
	data, err := encodeEvent(usageEvent(1, 2, 0.5))
	if err != nil {
		t.Fatal(err)
	}
	e, err := decodeEvent(data)
	if err != nil || e.Usage == nil || *e.Usage.Input != 1 || e.Usage.Source != taskrunner.UsageSourceClaudeResult {
		t.Fatalf("decode = %+v, %v", e, err)
	}
	if e, err := decodeEvent(`{"type":0,"text":"old"}`); err != nil || e.Usage != nil {
		t.Fatalf("old row decode = %+v, %v; want Usage nil", e, err)
	}
}

func TestUsageEvent_NoPromptOrSecret(t *testing.T) {
	const prompt = "PROMPT-SENTINEL-8d41"
	const secret = "sk-fake-SECRET-9f2c"
	t.Setenv("ANTHROPIC_API_KEY", secret)

	wm, st := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	reg := newTestRegistry(t, st)
	id, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, prompt, taskrunner.PermissionSettings{}, "")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	waitForStatus(t, reg, id, StatusDone, 5*time.Second)
	r, _ := reg.get(id)
	reg.record(r, usageEvent(10, 5, 0.1))

	row, err := st.GetRunUsage(id)
	if err != nil {
		t.Fatalf("GetRunUsage() error = %v", err)
	}
	rowJSON, _ := json.Marshal(row)
	evs, err := st.ListRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	var usageData string
	for _, se := range evs {
		if strings.Contains(se.EventData, `"usage"`) {
			usageData = se.EventData
		}
	}
	if usageData == "" {
		t.Fatal("no persisted usage event found")
	}
	for name, s := range map[string]string{"run_usage row": string(rowJSON), "usage event": usageData} {
		if strings.Contains(s, prompt) || strings.Contains(s, secret) {
			t.Errorf("%s leaks prompt or secret: %s", name, s)
		}
	}
}
