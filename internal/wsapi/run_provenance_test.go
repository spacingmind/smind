package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// seedProvenanceRuns persists two finished runs -- one with known O1
// provenance, one with none -- that a server built afterwards rehydrates
// exactly as if a previous daemon had written them.
func seedProvenanceRuns(t *testing.T, db *store.Store, taskID int64) {
	t.Helper()
	now := time.Now().UTC()
	for _, id := range []string{"prov-known", "prov-unknown"} {
		if _, err := db.CreateRun(store.Run{
			ID: id, TaskID: taskID, Provider: "glm", Prompt: "hi",
			Status: "done", StartedAt: now, FinishedAt: &now, StopReason: "end_turn",
		}); err != nil {
			t.Fatalf("store.CreateRun(%s) error = %v", id, err)
		}
	}
	base, head, dirty := "aaaa1111", "bbbb2222", true
	if err := db.UpdateRunProvenance("prov-known", store.RunProvenance{BaseSHA: &base, HeadSHA: &head, Dirty: &dirty}); err != nil {
		t.Fatalf("UpdateRunProvenance() error = %v", err)
	}
}

func TestRunList_ProvenanceFields(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	seedProvenanceRuns(t, db, task.ID)
	srv := newTestWSServer(t, wm, newTestRunner(wm), db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "list", "run.list", map[string]any{})
	resp := readEnvelopeFor(t, ws, "list", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("run.list error = %v", resp.Error.Message)
	}
	var list []map[string]any
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatalf("decode run.list result: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, r := range list {
		byID[r["ID"].(string)] = r
	}

	known := byID["prov-known"]
	if known == nil || known["BaseSHA"] != "aaaa1111" || known["HeadSHA"] != "bbbb2222" || known["Dirty"] != true {
		t.Fatalf("known run = %+v, want BaseSHA/HeadSHA/Dirty set", known)
	}
	unknown := byID["prov-unknown"]
	if unknown == nil {
		t.Fatalf("run.list = %+v, missing prov-unknown", list)
	}
	for _, key := range []string{"BaseSHA", "HeadSHA", "Dirty"} {
		v, ok := unknown[key]
		if !ok || v != nil {
			t.Errorf("unknown run %s = %v (present=%v), want JSON null", key, v, ok)
		}
	}

	// run.logs carries the same provenance under camelCase keys, omitted
	// while unknown.
	for id, wantKnown := range map[string]bool{"prov-known": true, "prov-unknown": false} {
		sendRequest(t, ws, id, "run.logs", map[string]any{"runId": id})
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("run.logs(%s) error = %v", id, resp.Error.Message)
		}
		var raw map[string]any
		if err := json.Unmarshal(resp.Result, &raw); err != nil {
			t.Fatalf("decode run.logs result: %v", err)
		}
		if wantKnown {
			if raw["baseSha"] != "aaaa1111" || raw["headSha"] != "bbbb2222" || raw["dirty"] != true {
				t.Errorf("run.logs(%s) = %+v, want baseSha/headSha/dirty", id, raw)
			}
			continue
		}
		for _, key := range []string{"baseSha", "headSha", "dirty"} {
			if _, ok := raw[key]; ok {
				t.Errorf("run.logs(%s) has %q, want it omitted while unknown", id, key)
			}
		}
	}
}

func TestRunStatusEvent_ProvenanceFields(t *testing.T) {
	t.Parallel()
	bus := newEventBus()
	sub := newSubscriber()
	sub.subscribe([]string{TopicRunStatus})
	bus.register(sub)
	n := busRunNotifier{bus: bus}

	base, head, dirty := "aaaa1111", "bbbb2222", false
	n.NotifyRunStatus(runs.RunStatus{ID: "r1", Status: runs.StatusDone, BaseSHA: &base, HeadSHA: &head, Dirty: &dirty})
	n.NotifyRunStatus(runs.RunStatus{ID: "r2", Status: runs.StatusRunning})

	sub.mu.Lock()
	queue := append([]Event(nil), sub.queue...)
	sub.mu.Unlock()
	if len(queue) != 2 {
		t.Fatalf("queued %d events, want 2", len(queue))
	}
	payload := func(e Event) map[string]any {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	known := payload(queue[0])
	// dirty=false is a known value and must survive omitempty (*bool).
	if known["baseSha"] != "aaaa1111" || known["headSha"] != "bbbb2222" || known["dirty"] != false {
		t.Fatalf("known payload = %+v, want baseSha/headSha/dirty=false", known)
	}
	unknown := payload(queue[1])
	for _, key := range []string{"baseSha", "headSha", "dirty"} {
		if _, ok := unknown[key]; ok {
			t.Errorf("unknown payload has %q, want it omitted: %+v", key, unknown)
		}
	}
}
