package wsapi

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDebugMemoryNotRunScopedAndRegistered(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	for _, m := range debugMemoryMethods {
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("%s is not registered", m)
		}
		for _, rs := range RunScopedMethods {
			if rs == m {
				t.Errorf("%s must not be run-scoped", m)
			}
		}
	}
	wantForbidden(t, callWS(t, e.identified(t), "debug.memory", nil), "")
}

func TestDebugMemoryRefusals(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	h := e.api.hs["debug.memory"]
	for name, rc := range map[string]*requestContext{
		"nil":        nil,
		"run scoped": {runScoped: true, local: true, channel: ChannelLoopback},
		"relay":      {local: false, channel: ChannelLoopback},
		"plain":      {local: true, channel: ChannelPlain},
		"tls remote": {local: true, channel: ChannelTLS},
	} {
		if res, err := h(context.Background(), rc, nil); err == nil || res != nil {
			t.Errorf("%s: answered (%v, %v), want forbidden", name, res, err)
		}
	}
}

func TestDebugMemoryReportsRegistryStats(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	res, err := e.api.hs["debug.memory"](context.Background(), localRC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	var got struct {
		PID int `json:"pid"`
		Mem struct {
			HeapAlloc uint64 `json:"heapAlloc"`
		} `json:"mem"`
		Runs struct {
			RunsInMemory int   `json:"runsInMemory"`
			RunningRuns  int   `json:"runningRuns"`
			BudgetBytes  int64 `json:"budgetBytes"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.PID == 0 || got.Mem.HeapAlloc == 0 || got.Runs.RunsInMemory < 1 || got.Runs.RunningRuns < 1 || got.Runs.BudgetBytes <= 0 {
		t.Fatalf("report incomplete: %s", b)
	}
}
