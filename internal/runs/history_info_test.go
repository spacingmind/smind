package runs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestHistoryInfo(t *testing.T) {
	// a full, legacy-style run: finished and stored, never converted
	s := newSkelEnv(t)
	s.seed(t, "r", []Event{
		{Type: taskrunner.EventTypeUserMessage, Text: "go"},
		{Type: taskrunner.EventTypeText, Text: "a"},
		{Type: taskrunner.EventTypeDone, StopReason: "end_turn"},
	})
	// a running run: refused before any skeleton column is read
	started := time.Now().UTC()
	if _, err := s.e.st.CreateRun(store.Run{ID: "busy", TaskID: s.e.task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: started}); err != nil {
		t.Fatal(err)
	}
	registerRunning(s.reg, "busy", s.e.task.ID, 0, started)

	// a skeleton run whose native_ref names a Claude Code session, rebuilder wired
	skel := newRebuildEnv(t, true)
	noRef := newRebuildEnv(t, true)
	if err := noRef.e.st.SetRunNativeRef("r", ""); err != nil {
		t.Fatal(err)
	}
	acp := newRebuildEnv(t, true)
	b, _ := json.Marshal(native.Ref{Provider: "claude-native", Harness: "acp", SessionID: rebuildSession})
	if err := acp.e.st.SetRunNativeRef("r", string(b)); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		got  HistoryInfo
		want HistoryInfo
	}{
		{"full legacy-style run", s.reg.HistoryInfo("r"), HistoryInfo{Source: "full"}},
		{"running run", s.reg.HistoryInfo("busy"), HistoryInfo{Source: "full"}},
		{"unknown run id", s.reg.HistoryInfo("missing"), HistoryInfo{Source: "full"}},
		{"skeleton, claude native_ref, rebuilder wired", skel.reg.HistoryInfo("r"),
			HistoryInfo{Source: "skeleton", Reason: "rebuild_not_requested", CanLoadFull: true}},
		{"skeleton, no rebuilder", newTestRegistry(t, skel.e.st).HistoryInfo("r"),
			HistoryInfo{Source: "skeleton", Reason: "provider_cannot_rebuild"}},
		{"skeleton, empty native_ref", noRef.reg.HistoryInfo("r"),
			HistoryInfo{Source: "skeleton", Reason: "provider_cannot_rebuild"}},
		{"skeleton, acp harness", acp.reg.HistoryInfo("r"),
			HistoryInfo{Source: "skeleton", Reason: "provider_cannot_rebuild"}},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, tc.got, tc.want)
		}
	}
}
