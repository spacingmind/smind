package wsapi

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

func TestFleetRead_CoalescedRunPagesWithoutLoss(t *testing.T) {
	e := newFleetEnv(t)
	st := e.fleetStore()
	ws, err := st.CreateWorkspace(store.Workspace{Path: t.TempDir(), Title: "coalesced reader", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "compare storage formats", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	seedCoalescedReaderRuns(t, st, task.ID, [3]string{"final ", "answer ", "here"})
	cl, caller := approvedConn(t, e)

	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			read := func(runID string, storedSeqs, visibleSeqs []int64) string {
				t.Helper()
				cursor := int64(-1)
				lastSeq := int64(-1)
				seen := make(map[int64]bool)
				var gotSeqs []int64
				var text strings.Builder
				var tools, done int
				for offset := 0; ; offset += limit {
					// Bound requests so a stuck cursor fails instead of hanging.
					if offset >= len(storedSeqs) {
						t.Fatalf("%s: pagination did not finish", runID)
					}
					var page wire.Activity
					params := wire.TaskActivityParams{Prov: okProv(caller), TaskID: task.ID, RunID: runID, Cursor: cursor, Limit: limit}
					if err := cl.Call(callCtx(), wire.MethodTaskActivity, params, &page); err != nil {
						t.Fatalf("%s cursor %d: %v", runID, cursor, err)
					}
					end := min(offset+limit, len(storedSeqs))
					wantCursor := storedSeqs[end-1]
					if page.RunID != runID || page.NextCursor != wantCursor || page.NextCursor <= cursor {
						t.Fatalf("%s: page=%+v, previous cursor=%d, want cursor=%d", runID, page, cursor, wantCursor)
					}
					if page.HasMore != (end < len(storedSeqs)) || len(page.Events) > limit {
						t.Fatalf("%s: unexpected page boundary: %+v", runID, page)
					}
					for _, event := range page.Events {
						if seen[event.Seq] || event.Seq <= lastSeq || event.Seq <= cursor || event.Seq > page.NextCursor {
							t.Fatalf("%s: repeated or unordered seq %d (last=%d, cursor=%d)", runID, event.Seq, lastSeq, cursor)
						}
						seen[event.Seq] = true
						lastSeq = event.Seq
						gotSeqs = append(gotSeqs, event.Seq)
						switch event.Type {
						case "text":
							text.WriteString(event.Text)
						case "tool_call":
							tools++
							if event.ToolName != "Read" || event.ToolStatus != "success" {
								t.Fatalf("%s: unexpected tool event: %+v", runID, event)
							}
						case "done":
							done++
							if event.StopReason != "end_turn" {
								t.Fatalf("%s: unexpected stop reason: %+v", runID, event)
							}
						default:
							t.Fatalf("%s: unexpected event: %+v", runID, event)
						}
					}
					cursor = page.NextCursor
					if !page.HasMore {
						break
					}
				}
				if !reflect.DeepEqual(gotSeqs, visibleSeqs) || tools != 1 || done != 1 {
					t.Fatalf("%s: seqs=%v, want %v; tools=%d, done=%d", runID, gotSeqs, visibleSeqs, tools, done)
				}
				// Resuming the final cursor must return no repeated rows.
				var exhausted wire.Activity
				if err := cl.Call(callCtx(), wire.MethodTaskActivity, wire.TaskActivityParams{
					Prov: okProv(caller), TaskID: task.ID, RunID: runID, Cursor: cursor, Limit: limit,
				}, &exhausted); err != nil {
					t.Fatal(err)
				}
				if len(exhausted.Events) != 0 || exhausted.HasMore || exhausted.NextCursor != cursor {
					t.Fatalf("%s: exhausted page=%+v", runID, exhausted)
				}
				return text.String()
			}
			legacy := read("legacy", []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, []int64{3, 4, 5, 6, 7, 8, 9, 10})
			coal := read("coal", []int64{0, 3, 6, 7, 10}, []int64{3, 6, 7, 10})
			if legacy != "Hello worldfinal answer here" || coal != legacy {
				t.Fatalf("text: legacy=%q, coalesced=%q", legacy, coal)
			}
		})
	}
}
