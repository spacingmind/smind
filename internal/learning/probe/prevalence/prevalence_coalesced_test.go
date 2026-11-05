package prevalence

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/learning/probe/signature"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestPrevalence_CoalescedEqualsLegacy(t *testing.T) {
	t.Parallel()
	f := newFx(t)
	chatID := f.chat(f.task.ID)
	svc := f.svc(nil)
	// A failing log over the excerpt threshold: the signature comes from its
	// first line, which skeleton form must hand back whole.
	bigFail := failMsg + "\n" + strings.Repeat("make[1]: *** [target] compile error in some/package/file.go\n", 50)
	stream := []map[string]any{
		{"type": int(taskrunner.EventTypeThinking), "text": "a"},
		{"type": int(taskrunner.EventTypeThinking), "text": "b"},
		{"type": int(taskrunner.EventTypeThinking), "text": "c"},
		{"type": int(taskrunner.EventTypeText), "text": "Hello "},
		{"type": int(taskrunner.EventTypeText), "text": "wor"},
		{"type": int(taskrunner.EventTypeText), "text": "ld"},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "failed", "toolName": "Bash", "toolStatus": taskrunner.ToolStatusFailure, "toolInput": map[string]any{"command": "make " + strings.Repeat("V=1 ", 375)}, "toolResult": bigFail},
		{"type": int(taskrunner.EventTypeToolCall), "toolCallId": "recovered", "toolName": "Bash", "toolStatus": taskrunner.ToolStatusSuccess, "toolInput": map[string]any{"command": "make"}, "toolResult": "ok"},
		{"type": int(taskrunner.EventTypeText), "text": "final "},
		{"type": int(taskrunner.EventTypeText), "text": "answer "},
		{"type": int(taskrunner.EventTypeText), "text": "here"},
		{"type": int(taskrunner.EventTypeDone), "stopReason": "end_turn"},
	}

	var derived [][]signature.Signature
	var scanned []Result
	for _, runID := range []string{"legacy", "coal", "skel"} {
		now := time.Now().UTC()
		if _, err := f.s.CreateRun(store.Run{ID: runID, TaskID: f.task.ID, ChatID: chatID, Provider: "claude-native", Status: "done", Purpose: "user", StartedAt: now, FinishedAt: &now}); err != nil {
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
			if _, err := f.s.AppendRunEvent(runID, int64(seq), string(data)); err != nil {
				t.Fatal(err)
			}
			seq = next
		}
		if runID == "skel" {
			if err := runs.Skeletonize(f.s, runID); err != nil {
				t.Fatal(err)
			}
			raw, err := f.s.ListRunEventsRaw(runID)
			if err != nil {
				t.Fatal(err)
			}
			moved := false
			for _, e := range raw {
				moved = moved || strings.Contains(e.EventData, `"v":2`)
			}
			if sk, err := f.s.GetRunSkeleton(runID); err != nil || sk.SkeletonAt == nil || !moved {
				t.Fatalf("skel run not converted: err=%v skeleton=%+v moved=%v", err, sk, moved)
			}
		}
	}
	// Every proposal points at the recovery's original seq, unchanged by merging.
	for _, runID := range []string{"legacy", "coal", "skel"} {
		evidence, err := json.Marshal([]gate.Evidence{{Kind: gate.EvFailureRecovery, RunID: runID, Seq: 7, Quote: "q"}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.s.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: "create", Source: "reflector", Name: runID, Evidence: string(evidence)})
		if err != nil {
			t.Fatal(err)
		}
		sigs, err := svc.Derive(p, nil)
		if err != nil {
			t.Fatal(err)
		}
		derived = append(derived, sigs)
		result, err := svc.Scan(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		scanned = append(scanned, result)
	}
	want := []signature.Signature{{Kind: gate.EvFailureRecovery, Tool: "Bash", Detail: signature.Normalise(failMsg)}}
	if !reflect.DeepEqual(derived[0], derived[1]) || !reflect.DeepEqual(derived[0], derived[2]) || !reflect.DeepEqual(derived[0], want) {
		t.Fatalf("derived legacy/coal/skel = %+v; want %+v", derived, want)
	}
	if !reflect.DeepEqual(scanned[0], scanned[1]) || !reflect.DeepEqual(scanned[0], scanned[2]) || len(scanned[0].Signatures) != 1 {
		t.Fatalf("scan legacy/coal/skel = %+v", scanned)
	}
	count := scanned[0].Signatures[0]
	if count.Runs != 3 || count.Chats != 1 || count.LastSeenAt == nil || count.Truncated {
		t.Fatalf("prevalence = %+v; want three runs in one chat", count)
	}
}
