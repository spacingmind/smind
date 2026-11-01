package wsapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/taskrunner"
)

func heredocToolEvent() taskrunner.Event {
	cmd := "cat <<'EOF' > notes.md\nkey=SECRET_TOKEN_123\nEOF\n&& git status"
	in, _ := json.Marshal(map[string]any{"command": cmd, "description": "write notes"})
	return taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "t1", ToolName: "execute", ToolTitle: "Run " + cmd, ToolStatus: "running", ToolInput: in}
}

// Every client path (live tool_call, run.logs, and the relay/timeline/MCP
// views built on them) goes through toRunToolCallParams: no body may leave it.
func TestToolCallToClients_NeverCarriesHeredocBody(t *testing.T) {
	t.Parallel()
	e := heredocToolEvent()
	live, _ := json.Marshal(toRunToolCallParams(e))
	logs, _ := json.Marshal(toRunLogEvent(e))
	for name, raw := range map[string][]byte{"live tool_call": live, "run.logs": logs} {
		if strings.Contains(string(raw), "SECRET_TOKEN_123") {
			t.Errorf("%s carries the body: %s", name, raw)
		}
		if !strings.Contains(string(raw), "[heredoc ") || !strings.Contains(string(raw), `"heredocs":[{"index":0,"bytes":`) {
			t.Errorf("%s lacks the marker or refs: %s", name, raw)
		}
		if !strings.Contains(string(raw), "write notes") || !strings.Contains(string(raw), "git status") {
			t.Errorf("%s dropped other input: %s", name, raw)
		}
	}
	if !strings.Contains(string(e.ToolInput), "SECRET_TOKEN_123") {
		t.Fatal("the raw event must keep its body on the daemon")
	}
}

func TestToolCallToClients_NoHeredocIsByteIdentical(t *testing.T) {
	t.Parallel()
	in := json.RawMessage(`{"command":"go test ./...","x":1}`)
	p := toRunToolCallParams(taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolInput: in, ToolTitle: "Run go test"})
	if string(p.Input) != string(in) || p.Title != "Run go test" || p.Heredocs != nil {
		t.Errorf("params = %+v", p)
	}
}
