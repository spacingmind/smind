package probesvc

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/probe/contrast"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestArm_CoalescedEqualsLegacy(t *testing.T) {
	t.Parallel()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	ws, err := st.CreateWorkspace(store.Workspace{Path: "/repo", Title: "w", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{d: Deps{Store: st}}
	stream := []map[string]any{
		{"type": int(taskrunner.EventTypeThinking), "text": "a"},
		{"type": int(taskrunner.EventTypeThinking), "text": "b"},
		{"type": int(taskrunner.EventTypeThinking), "text": "c"},
		{"type": int(taskrunner.EventTypeText), "text": "Hello "},
		{"type": int(taskrunner.EventTypeText), "text": "wor"},
		{"type": int(taskrunner.EventTypeText), "text": "ld"},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "call", "toolName": "Bash", "toolStatus": taskrunner.ToolStatusFailure, "toolInput": map[string]any{"command": "go test " + strings.Repeat("-race ", 250)}, "toolResult": "permission denied\n" + strings.Repeat("--- FAIL: TestSomething (0.00s) file_test.go:12: boom\n", 55)},
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}

	type outcome struct {
		final, thinking, text string
		metrics               contrast.Metrics
	}
	var outcomes []outcome
	for _, runID := range []string{"legacy", "coal", "skel"} {
		now := time.Now().UTC()
		if _, err := st.CreateRun(store.Run{ID: runID, TaskID: task.ID, Provider: "claude-native", Status: "done", Purpose: store.RunPurposeProbe, StartedAt: now, FinishedAt: &now}); err != nil {
			t.Fatal(err)
		}
		for seq := 0; seq < len(stream); {
			row := make(map[string]any)
			for key, value := range stream[seq] {
				row[key] = value
			}
			next := seq + 1
			if runID == "coal" && (row["type"] == int(taskrunner.EventTypeText) || row["type"] == int(taskrunner.EventTypeThinking)) {
				for next < len(stream) && stream[next]["type"] == row["type"] {
					row["text"] = row["text"].(string) + stream[next]["text"].(string)
					next++
				}
			}
			data, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.AppendRunEvent(runID, int64(seq), string(data)); err != nil {
				t.Fatal(err)
			}
			seq = next
		}

		if runID == "skel" {
			if err := runs.Skeletonize(st, runID); err != nil {
				t.Fatal(err)
			}
			raw, err := st.ListRunEventsRaw(runID)
			if err != nil {
				t.Fatal(err)
			}
			moved := false
			for _, e := range raw {
				moved = moved || strings.Contains(e.EventData, `"v":2`)
			}
			if sk, err := st.GetRunSkeleton(runID); err != nil || sk.SkeletonAt == nil || !moved {
				t.Fatalf("skel run not converted: err=%v skeleton=%+v moved=%v", err, sk, moved)
			}
		}

		events, final := svc.collectEvents(runID)
		got := outcome{final: final, metrics: contrast.ComputeMetrics(contrast.ArmInput{Events: events})}
		for _, ev := range events {
			switch ev.E.Type {
			case taskrunner.EventTypeThinking:
				got.thinking += ev.E.Text
			case taskrunner.EventTypeText:
				got.text += ev.E.Text
			case taskrunner.EventTypeToolCall:
				if ev.Seq != 6 {
					t.Fatalf("%s tool seq = %d, want 6", runID, ev.Seq)
				}
			}
		}
		if len(events) == 0 || events[len(events)-1].Seq != 10 || events[len(events)-1].E.StopReason != "end_turn" {
			t.Fatalf("%s terminal event missing: %+v", runID, events)
		}
		if runID == "skel" {
			// Thinking text is not kept in skeleton form (ADR-0053 H1).
			if !strings.HasPrefix(got.thinking, "[thinking not kept:") {
				t.Fatalf("skel thinking = %q, want the placeholder", got.thinking)
			}
			got.thinking = "abc"
		}
		outcomes = append(outcomes, got)
	}
	want := outcome{final: "final answer here", thinking: "abc", text: "Hello worldfinal answer here", metrics: contrast.Metrics{ToolCalls: 1, FailedToolCalls: 1, PermissionDenials: 1}}
	if outcomes[0] != outcomes[1] || outcomes[0] != outcomes[2] || outcomes[0] != want {
		t.Fatalf("legacy/coal/skel outcomes = %+v; want %+v", outcomes, want)
	}
}
