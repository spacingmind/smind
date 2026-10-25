package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// run.start for claude-native with no model resolved is refused with the typed
// no_model error from every caller (web/CLI human, MCP agent), and nothing is
// created or queued: the CLI is never reached.
func TestUnpinnedClaude_NoModelResolved_RefusedAtRunStart(t *testing.T) {
	t.Parallel()
	e := newAttachEnv(t, "", false)
	for _, tc := range []struct {
		name   string
		params map[string]any
		rc     *requestContext
	}{
		{"web, no model key", map[string]any{}, &requestContext{}},
		{"web, empty model", map[string]any{"model": ""}, &requestContext{}},
		{"cli, queue when busy", map[string]any{"whenBusy": "queue"}, &requestContext{}},
		{"mcp agent", map[string]any{}, &requestContext{runScoped: true, ident: runIdentity{taskID: e.task.ID, chatID: 1}}},
	} {
		params := map[string]any{"taskId": e.task.ID, "provider": "claude-native", "prompt": "x"}
		for k, v := range tc.params {
			params[k] = v
		}
		raw, _ := json.Marshal(params)
		_, err := e.api.hs["run.start"](context.Background(), tc.rc, raw)
		if err == nil || !strings.Contains(err.Error(), "no_model: no model resolved for claude-native") {
			t.Fatalf("%s: error = %v, want the no_model refusal", tc.name, err)
		}
	}
	if n := e.runCount(t); n != 0 {
		t.Fatalf("runs = %d, want 0", n)
	}
	if items := e.queueOf(t); len(items) != 0 {
		t.Fatalf("queue = %+v, want empty", items)
	}
}
