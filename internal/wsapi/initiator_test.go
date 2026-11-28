package wsapi

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/workspace"
)

// The human RPCs mark their context only for a caller on a user surface:
// a run-scoped agent connection (and a nil requestContext) must keep the
// daemon's own, stripped git.

func Test_task_commit_marks_human_only_for_a_non_run_scoped_caller(t *testing.T) {
	t.Parallel()
	ctx := humanContext(context.Background(), &requestContext{runScoped: false})
	if !workspace.IsHumanInitiated(ctx) {
		t.Fatal("a user-surface caller's context is not human-initiated")
	}
	ctx = humanContext(context.Background(), &requestContext{runScoped: true})
	if workspace.IsHumanInitiated(ctx) {
		t.Fatal("a run-scoped agent caller's context is human-initiated")
	}
	if workspace.IsHumanInitiated(humanContext(context.Background(), nil)) {
		t.Fatal("a nil requestContext marked the context human-initiated")
	}
}

// Test_initiator_marks_every_human_rpc_manager_call reads the three handler
// files and asserts that each of the six Manager calls that is a human
// action sits in a handler that marked its context with humanContext first:
// a handler that forgets the mark silently runs the person's action with
// the daemon's stripped git (no hook, no filter, no merge driver).
func Test_initiator_marks_every_human_rpc_manager_call(t *testing.T) {
	t.Parallel()
	cases := []struct{ file, call string }{
		{"handlers.go", "wm.TaskStageAs("},
		{"handlers.go", "wm.CommitTaskAs("},
		{"sync_handlers.go", "h.wm.Push("},
		{"sync_handlers.go", "h.wm.UpdateFromBase("},
		{"pr_handlers.go", "h.wm.PushForPR("},
		{"pr_handlers.go", "h.wm.PushUpdate("},
	}
	for _, c := range cases {
		src, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		text := string(src)
		found := false
		for idx := 0; idx < len(text); {
			i := strings.Index(text[idx:], c.call)
			if i < 0 {
				break
			}
			found = true
			at := idx + i
			// The enclosing handler: the last top-level "func" before the
			// call (a returned closure belongs to the function that
			// returns it).
			fn := strings.LastIndex(text[:at], "\nfunc ")
			switch {
			case fn < 0:
				t.Errorf("%s: %s at byte %d sits outside any function", c.file, c.call, at)
			case !strings.Contains(text[fn:at], "humanContext("):
				t.Errorf("%s: %s is not preceded by humanContext( in its handler", c.file, c.call)
			}
			idx = at + len(c.call)
		}
		if !found {
			t.Errorf("%s: no %s call found", c.file, c.call)
		}
	}
}
