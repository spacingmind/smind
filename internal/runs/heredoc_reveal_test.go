package runs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

func heredocCmd() string {
	return "cat <<EOF > f\nfirst SECRET_TOKEN_123\nEOF\n( cat <<B\nsecond body\nB\n)"
}

func TestPendingHeredoc_OnlyWhilePending(t *testing.T) {
	t.Parallel()
	reg, r, runID, _ := policyRun(t, taskrunner.PermissionSettings{}, "-", false)
	d := runPermissionDecider{reg: reg, r: r}
	tc, _ := json.Marshal(map[string]any{"kind": "execute", "rawInput": map[string]any{"command": heredocCmd()}})
	out, reqID := decideAsync(t, d, taskrunner.BuildPermissionDetail(t.TempDir(), tc))

	got, err := reg.PendingHeredoc(reqID, 0)
	if err != nil || got.RunID != runID || !strings.Contains(got.Text, "first SECRET_TOKEN_123") {
		t.Fatalf("PendingHeredoc 0 = %+v, %v", got, err)
	}
	if got, err := reg.PendingHeredoc(reqID, 1); err != nil || got.Text != "second body\n" {
		t.Fatalf("PendingHeredoc 1 = %+v, %v", got, err)
	}
	for _, i := range []int{-1, 2} {
		if _, err := reg.PendingHeredoc(reqID, i); !errors.Is(err, ErrNoHeredoc) {
			t.Errorf("index %d: err = %v, want ErrNoHeredoc", i, err)
		}
	}
	if _, err := reg.PendingHeredoc("nope", 0); !errors.Is(err, ErrNoHeredoc) {
		t.Errorf("unknown id: err = %v", err)
	}
	if err := reg.RespondPermission(runID, reqID, "allow-1"); err != nil {
		t.Fatal(err)
	}
	<-out
	if _, err := reg.PendingHeredoc(reqID, 0); !errors.Is(err, ErrNoHeredoc) {
		t.Errorf("after resolution: err = %v, want ErrNoHeredoc", err)
	}
}

// A tool call's body is revealable live and, for a finished run, from the
// persisted log by a registry that never held the run (a daemon restart).
func TestToolCallHeredoc_LiveAndFromDisk(t *testing.T) {
	t.Parallel()
	reg, r, runID := startedRun(t, taskrunner.PermissionSettings{})
	in, _ := json.Marshal(map[string]any{"command": heredocCmd()})
	reg.record(r, taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "call-1", ToolName: "execute", ToolInput: in})
	reg.record(r, taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "call-1", ToolStatus: "completed"}) // update without input

	for name, g := range map[string]*Registry{"live": reg, "from disk": newTestRegistry(t, reg.st)} {
		got, err := g.ToolCallHeredoc(runID, "call-1", 1)
		if err != nil || got.Text != "second body\n" {
			t.Fatalf("%s: = %+v, %v", name, got, err)
		}
		for _, bad := range []struct {
			run, call string
			i         int
		}{{runID, "call-9", 0}, {runID, "call-1", 2}, {"run-none", "call-1", 0}} {
			if _, err := g.ToolCallHeredoc(bad.run, bad.call, bad.i); !errors.Is(err, ErrNoHeredoc) {
				t.Errorf("%s %+v: err = %v, want ErrNoHeredoc", name, bad, err)
			}
		}
	}
}
