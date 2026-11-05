package wsapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/store/storetest"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func openRetStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRunLogsPrunedMarker(t *testing.T) {
	st := openRetStore(t)
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "the final answer", 6)
	reg, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	reg.SetRetentionDays(30)
	for _, tail := range []int{0, 2, 50} {
		raw, _ := json.Marshal(map[string]any{"runId": p.RunID, "tail": tail})
		res, err := handleRunLogs(reg)(context.Background(), nil, raw)
		if err != nil {
			t.Fatalf("tail=%d: run.logs on a pruned run errored: %v", tail, err)
		}
		b, _ := json.Marshal(res)
		var got struct {
			HistoryPruned   bool          `json:"historyPruned"`
			HistoryPrunedAt string        `json:"historyPrunedAt"`
			Status          string        `json:"status"`
			Events          []runLogEvent `json:"events"`
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if !got.HistoryPruned || got.HistoryPrunedAt == "" || got.Status != "done" {
			t.Fatalf("tail=%d: %s", tail, b)
		}
		var text strings.Builder
		for _, e := range got.Events {
			text.WriteString(e.Text)
		}
		if len(got.Events) == 0 || !strings.Contains(text.String(), "the final answer") || (tail != 2 && !strings.Contains(text.String(), "pruned after 30 days")) {
			t.Fatalf("tail=%d: events = %s", tail, b)
		}
	}
}

// The steward's handoff reader returns the same text for a pruned run as for
// the same run unpruned (equivalence for what the summary keeps).
func TestStewardFinalRunText_PrunedEqualsFull(t *testing.T) {
	st := openRetStore(t)
	// A full run with the same events SeedPrunedRun writes (text e0..e4).
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "e0e1e2e3e4", 5)
	if _, err := st.CreateRun(store.Run{ID: "full", TaskID: p.TaskRowID, Provider: "glm", Prompt: "p", Status: "done", Purpose: "user", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := st.AppendRunEvent("full", int64(i), `{"type":0,"text":"e`+string(rune('0'+i))+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	want, err := finalRunText(st, "full")
	if err != nil || want != "e0e1e2e3e4" {
		t.Fatalf("full run text = %q, %v", want, err)
	}
	got, err := finalRunText(st, p.RunID)
	if err != nil || got != want {
		t.Fatalf("pruned run text = %q, %v; want %q", got, err, want)
	}
}

func TestFleetRunEvents_PrunedRunGetsSummary(t *testing.T) {
	st := openRetStore(t)
	p := storetest.SeedPrunedRun(t, st, "pruned-1", "answer", 4)
	src := &fleetSource{db: st}
	rows, err := src.RunEvents(p.RunID, -1, 50)
	if err != nil || len(rows) != 3 {
		t.Fatalf("RunEvents = %d rows, %v; want note, text, done", len(rows), err)
	}
	var types []taskrunner.EventType
	for _, r := range rows {
		e, err := runs.DecodeStoredEvent(r.EventData)
		if err != nil {
			t.Fatal(err)
		}
		types = append(types, e.Type)
	}
	if types[0] != taskrunner.EventTypeSessionNote || types[1] != taskrunner.EventTypeText || types[2] != taskrunner.EventTypeDone {
		t.Fatalf("types = %v", types)
	}
	if more, _ := src.RunEvents(p.RunID, 1, 50); len(more) != 1 || more[0].Seq != 2 {
		t.Fatalf("after seq 1 = %+v", more)
	}
}

func TestRetentionRPCs_RegisteredAndGuarded(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	for _, m := range retentionMethods {
		if _, ok := e.api.hs[m]; !ok {
			t.Errorf("%s is not registered", m)
		}
		for _, rs := range RunScopedMethods {
			if rs == m {
				t.Errorf("%s must not be run-scoped", m)
			}
		}
		h := e.api.hs[m]
		for name, rc := range map[string]*requestContext{
			"nil": nil, "run scoped": {runScoped: true, local: true, channel: ChannelLoopback},
			"relay": {local: false, channel: ChannelLoopback}, "plain": {local: true, channel: ChannelPlain},
		} {
			if res, err := h(context.Background(), rc, json.RawMessage(`{"dryRun":true}`)); err == nil || res != nil {
				t.Errorf("%s as %s answered (%v, %v), want forbidden", m, name, res, err)
			}
		}
		wantForbidden(t, callWS(t, e.identified(t), m, nil), "")
	}
	// db.compact refuses while a run is running (the seat run of this env).
	if _, err := e.api.hs["db.compact"](context.Background(), localRC(), nil); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("db.compact with a running run = %v, want a refusal", err)
	}
	// A dry run answers with a preview and changes nothing.
	res, err := e.api.hs["runs.prune"](context.Background(), localRC(), json.RawMessage(`{"dryRun":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if rep, ok := res.(runs.PruneReport); !ok || !rep.DryRun {
		t.Fatalf("dry run result = %#v", res)
	}
}
