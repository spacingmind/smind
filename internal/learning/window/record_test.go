package window

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func TestWindow_ExcludesEnvMcpThinkingAndSecretPathReads(t *testing.T) {
	b := &evBuilder{}
	b.add(taskrunner.Event{Type: taskrunner.EventTypeThinking, Text: "THINKING-LEAK"})
	b.add(taskrunner.Event{Type: taskrunner.EventTypeMcpServers, McpServers: []taskrunner.McpServerState{{Name: "MCP-LEAK"}}})
	b.add(taskrunner.Event{Type: taskrunner.EventTypeRaw, RawKind: "plan", RawPayload: jraw("RAW-LEAK")})
	b.add(taskrunner.Event{Type: taskrunner.EventTypeUsage, Usage: &taskrunner.Usage{}})
	b.add(taskrunner.Event{Type: taskrunner.EventTypeSessionNote, Text: "NOTE-LEAK"})
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionModeChanged, Text: "MODE-LEAK"})
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "q", PermissionSummary: "REQ-LEAK"})
	b.add(taskrunner.Event{Type: taskrunner.EventTypeDone, StopReason: "end_turn"})
	b.tool("1", "Read", taskrunner.ToolStatusFailure, map[string]any{"file_path": "/repo/.env"}, "ENVBODY=hunter2 no such file")
	b.tool("2", "Read", taskrunner.ToolStatusFailure, map[string]any{"file_path": "~/.ssh/id_rsa"}, "KEYBODY")
	b.tool("3", "Bash", taskrunner.ToolStatusFailure, map[string]any{"command": "cat ~/.aws/credentials"}, "AWSBODY")
	out := build(humanRun("r1", "please fix the build", b.events()))
	for _, leak := range []string{"THINKING-LEAK", "MCP-LEAK", "RAW-LEAK", "NOTE-LEAK", "MODE-LEAK", "REQ-LEAK", "ENVBODY", "KEYBODY", "AWSBODY"} {
		if strings.Contains(out.Text, leak) {
			t.Errorf("window leaks %q:\n%s", leak, out.Text)
		}
	}
	if n := strings.Count(out.Text, "result withheld (secret path)"); n != 3 {
		t.Errorf("withheld markers=%d want 3:\n%s", n, out.Text)
	}
}

func TestWindow_CapsRecordInputResultAndTotal(t *testing.T) {
	b := &evBuilder{}
	big := strings.Repeat("x", 5000)
	b.tool("1", "Read", taskrunner.ToolStatusSuccess, map[string]any{"file_path": "/repo/a.go"}, "FILEBODY"+big)
	b.tool("2", "Read", taskrunner.ToolStatusFailure, map[string]any{"file_path": "/repo/b.go"}, "RDFAIL"+big)
	b.tool("3", "Bash", taskrunner.ToolStatusFailure, map[string]any{"command": strings.Repeat("c", 3000)}, strings.Repeat("e", 6000))
	b.tool("4", "Grep", taskrunner.ToolStatusSuccess, map[string]any{"pattern": "p"}, "GREPRESULT")
	b.text(strings.Repeat("a", 10000))
	out := build(humanRun("r1", strings.Repeat("u", 10000), b.events()))
	if strings.Contains(out.Text, "FILEBODY") || strings.Contains(out.Text, "GREPRESULT") {
		t.Errorf("success results rendered:\n%s", out.Text)
	}
	for _, line := range strings.Split(out.Text, "\n") {
		if len(line) > 4096 {
			t.Errorf("record line %d bytes > 4096: %.60s", len(line), line)
		}
	}
	rd := lineWith(out.Text, "[TOOL #2")
	if i := strings.Index(rd, "RDFAIL"); i < 0 {
		t.Fatalf("read failure missing: %q", rd)
	} else if n := len(rd[i:]); n > 200 {
		t.Errorf("read failure result %d bytes > 200", n)
	}
	bash := lineWith(out.Text, "[TOOL #3")
	cs := strings.Count(bash, "c")
	es := strings.Count(bash, "e")
	if cs > 1024 {
		t.Errorf("input %d > 1024", cs)
	}
	if es > 2048+len("error")+10 || es < 1500 {
		t.Errorf("failure result e-count %d", es)
	}
	small := Caps{RecordBytes: 4096, InputBytes: 1024, ResultBytes: 1024, FailureResultBytes: 2048, WindowBytes: 120 * 1024}
	if len(out.Text) > small.WindowBytes {
		t.Errorf("total %d", len(out.Text))
	}
}

func lineWith(text, sub string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestWindow_RedactsBeforeClipping(t *testing.T) {
	caps := Caps{RecordBytes: 400, InputBytes: 100, ResultBytes: 100, FailureResultBytes: 200, WindowBytes: 100000}
	// Final clip boundary: body cap is RecordBytes - len(prefix); put the secret across it.
	for pad := 300; pad < 420; pad++ {
		p := strings.Repeat("a", pad) + secret + strings.Repeat("b", 50)
		out := Build(Input{Runs: []Run{humanRun("r1", p, nil)}, Redact: fakeRedact, Caps: caps})
		if strings.Contains(out.Text, "SECRET") {
			t.Fatalf("pad %d leaks secret fragment: %q", pad, out.Text)
		}
	}
	// 4x pre-clip boundary: secret straddling 4*cap of the body cap.
	for pad := 1500; pad < 1700; pad++ {
		p := strings.Repeat("a", pad) + secret
		out := Build(Input{Runs: []Run{humanRun("r1", p, nil)}, Redact: fakeRedact, Caps: caps})
		if strings.Contains(out.Text, "SECRET") {
			t.Fatalf("pad %d leaks at pre-clip boundary", pad)
		}
	}
	// Heavily redacted text shrinks: the unredacted tail of a pre-clipped
	// string must not survive.
	p := strings.Repeat(secret+" ", 200) + "tailSECRETSECRE"
	out := Build(Input{Runs: []Run{humanRun("r1", p, nil)}, Redact: fakeRedact, Caps: caps})
	if strings.Contains(out.Text, "SECRET") {
		t.Errorf("shrunk redaction leaks tail fragment: %q", out.Text)
	}
}

func TestWindow_NilRedactFailsClosed(t *testing.T) {
	b := &evBuilder{}
	b.text("hello there")
	out := Build(Input{Runs: []Run{humanRun("r1", "prompt text here", b.events())}, Caps: defCaps()})
	if out.Text != "" || len(out.Window) != 0 {
		t.Errorf("nil Redact rendered %q", out.Text)
	}
}

func TestWindow_EditsRenderPathAndCountsOnly(t *testing.T) {
	b := &evBuilder{}
	b.tool("1", "Edit", taskrunner.ToolStatusSuccess, map[string]any{"file_path": "/repo/a.go", "old_string": "l1\nl2", "new_string": "n1\nn2\nn3\nn4"}, "ok EDITRESULT")
	b.tool("2", "Write", taskrunner.ToolStatusSuccess, map[string]any{"file_path": "/repo/b.go", "content": "WRITEBODY\nline2\nline3\n"}, nil)
	b.tool("3", "MultiEdit", taskrunner.ToolStatusSuccess, map[string]any{"file_path": "/repo/c.go", "edits": []map[string]any{
		{"old_string": "a", "new_string": "b\nc"}, {"old_string": "x\ny", "new_string": "z"}}}, nil)
	b.tool("4", "edit", taskrunner.ToolStatusSuccess, map[string]any{"path": "/repo/d.go", "old_string": "q", "new_string": "r"}, nil)
	out := build(humanRun("r1", "edit things", b.events()))
	for _, bad := range []string{"EDITRESULT", "WRITEBODY", "n1", "l1"} {
		if strings.Contains(out.Text, bad) {
			t.Errorf("edit content %q rendered:\n%s", bad, out.Text)
		}
	}
	for _, want := range []string{"path=/repo/a.go +4/-2 lines", "path=/repo/b.go +3/-0 lines", "path=/repo/c.go +3/-3 lines", "path=/repo/d.go +1/-1 lines"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("missing %q:\n%s", want, out.Text)
		}
	}
}

func TestWindow_AssistantChunksMerge(t *testing.T) {
	b := &evBuilder{}
	b.text("part one, ").text("part two, ").text("part three.")
	b.tool("1", "Bash", taskrunner.ToolStatusSuccess, map[string]any{"command": "ls"}, nil)
	b.text("second turn")
	out := build(humanRun("r1", "go", b.events()))
	if !strings.Contains(out.Text, "[ASSISTANT #1] part one, part two, part three.") {
		t.Errorf("chunks not merged:\n%s", out.Text)
	}
	if strings.Count(out.Text, "[ASSISTANT #") != 2 || !strings.Contains(out.Text, "[ASSISTANT #5] second turn") {
		t.Errorf("turn split wrong:\n%s", out.Text)
	}
	if got := out.Window[gate.EventKey{RunID: "r1", Seq: 1}]; got.Kind != "assistant" {
		t.Errorf("assistant gate kind %q", got.Kind)
	}
}

func TestWindow_ToolEventsMergeByID(t *testing.T) {
	b := &evBuilder{}
	b.tool("c1", "Bash", taskrunner.ToolStatusRunning, map[string]any{"command": "go test ./..."}, nil)
	b.add(taskrunner.Event{Type: taskrunner.EventTypeToolCall, ToolCallID: "c1", ToolStatus: taskrunner.ToolStatusFailure, ToolResult: jraw("FAIL pkg")})
	out := build(humanRun("r1", "run tests", b.events()))
	l := lineWith(out.Text, "[TOOL #")
	if !strings.HasPrefix(l, "[TOOL #1 Bash failure]") || !strings.Contains(l, "go test ./...") || !strings.Contains(l, "error: FAIL pkg") {
		t.Errorf("merged record wrong: %q", l)
	}
	if strings.Count(out.Text, "[TOOL #") != 1 {
		t.Errorf("duplicate tool records:\n%s", out.Text)
	}
	if got := out.Window[gate.EventKey{RunID: "r1", Seq: 1}]; got.Kind != "tool_output" {
		t.Errorf("tool gate kind %q", got.Kind)
	}
}

func TestWindow_UserRecordAndAgentUserChunk(t *testing.T) {
	b := &evBuilder{}
	b.add(taskrunner.Event{Type: taskrunner.EventTypeUserMessage, Text: "synthesized follow up"})
	out := build(humanRun("r1", "typed by a human", b.events()))
	if got := out.Window[gate.EventKey{RunID: "r1", Seq: -1}]; got.Kind != gate.EvUserMessage || got.Text != "typed by a human" {
		t.Errorf("prompt gate event %+v", got)
	}
	if got := out.Window[gate.EventKey{RunID: "r1", Seq: 1}]; got.Kind == gate.EvUserMessage {
		t.Errorf("agent user chunk qualifies as user_message")
	}
	if !strings.Contains(out.Text, "[USER origin=agent #1] synthesized follow up") {
		t.Errorf("agent user chunk record missing:\n%s", out.Text)
	}
}

func TestWindow_PermissionDenialNotLeakingRequestBody(t *testing.T) {
	// covered in step 4; here only ensure unresolved requests never render.
	b := &evBuilder{}
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "q", PermissionSummary: "run rm -rf"})
	out := build(humanRun("r1", "do it", b.events()))
	if strings.Contains(out.Text, "rm -rf") {
		t.Errorf("request rendered")
	}
}
