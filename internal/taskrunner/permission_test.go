package taskrunner

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
	"github.com/spacingmind/smind/internal/acp"
)

// recordingDecider records exactly the summary/options it was called with
// and returns a fixed optionID, for unit-testing the adapters' conversion
// logic in isolation from any real subprocess.
type recordingDecider struct {
	optionID string
	err      error

	gotSummary string
	gotCommand string
	gotOptions []PermissionOption
}

func (d *recordingDecider) Decide(_ context.Context, summary, command string, options []PermissionOption) (string, error) {
	d.gotSummary = summary
	d.gotCommand = command
	d.gotOptions = options
	return d.optionID, d.err
}

func TestACPDeciderAdapter_TranslatesOptionsAndChoice(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{optionID: "opt-2"}
	adapter := acpDeciderAdapter{decider: d}

	req := acp.RequestPermissionParams{
		SessionID: "sess-1",
		ToolCall:  json.RawMessage(`{"toolCallId":"tc-1","title":"Delete a file"}`),
		Options: []acp.PermissionOption{
			{OptionID: "opt-1", Name: "Allow once", Kind: acp.PermissionAllowOnce},
			{OptionID: "opt-2", Name: "Reject once", Kind: acp.PermissionRejectOnce},
		},
	}

	got, err := adapter.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got != "opt-2" {
		t.Fatalf("Decide() = %q, want %q", got, "opt-2")
	}

	if d.gotSummary != "Delete a file" {
		t.Fatalf("summary = %q, want %q", d.gotSummary, "Delete a file")
	}
	if d.gotCommand != "" {
		t.Fatalf("command = %q, want empty -- ACP's ToolCall carries no confirmed command field", d.gotCommand)
	}
	want := []PermissionOption{
		{ID: "opt-1", Label: "Allow once", Kind: "allow_once"},
		{ID: "opt-2", Label: "Reject once", Kind: "reject_once"},
	}
	if !reflect.DeepEqual(d.gotOptions, want) {
		t.Fatalf("options = %+v, want %+v", d.gotOptions, want)
	}
}

func TestACPDeciderAdapter_PropagatesError(t *testing.T) {
	t.Parallel()
	wantErr := context.Canceled
	d := &recordingDecider{err: wantErr}
	adapter := acpDeciderAdapter{decider: d}

	_, err := adapter.Decide(context.Background(), acp.RequestPermissionParams{})
	if err != wantErr {
		t.Fatalf("Decide() error = %v, want %v", err, wantErr)
	}
}

func TestSummarizeACPToolCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"title present", `{"title":"Run a command","toolCallId":"tc-1"}`, "Run a command"},
		{"only toolCallId", `{"toolCallId":"tc-1"}`, "tool call tc-1"},
		{"neither", `{}`, "tool call"},
		{"malformed json", `not json`, "tool call"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := summarizeACPToolCall(json.RawMessage(tt.raw)); got != tt.want {
				t.Fatalf("summarizeACPToolCall(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestClaudeDeciderAdapter_Allow proves an "allow" choice translates into
// the real (allow, updatedInput, denyMessage, err) tuple Claude Code's
// protocol expects: allow=true, the original input carried through
// unchanged (this pass never lets a human edit the input -- see the
// PermissionDecider interface's ID-only shape), and no deny message.
func TestClaudeDeciderAdapter_Allow(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{optionID: claudeOptionAllow}
	adapter := claudeDeciderAdapter{decider: d}

	req := claudecode.CanUseToolRequest{
		ToolName:  "Bash",
		Input:     map[string]any{"command": "echo hi"},
		ToolUseID: "tool-1",
	}

	allow, updatedInput, denyMessage, updatedPermissions, interrupt, err := adapter.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !allow {
		t.Fatal("allow = false, want true")
	}
	if !reflect.DeepEqual(updatedInput, req.Input) {
		t.Fatalf("updatedInput = %+v, want unchanged %+v", updatedInput, req.Input)
	}
	if denyMessage != "" {
		t.Fatalf("denyMessage = %q, want empty on allow", denyMessage)
	}
	if updatedPermissions != nil || interrupt {
		t.Fatalf("updatedPermissions/interrupt = %+v/%v, want nil/false -- this pass never sets them", updatedPermissions, interrupt)
	}

	want := []PermissionOption{
		{ID: "allow", Label: "Allow", Kind: "allow_once"},
		{ID: "deny", Label: "Deny", Kind: "reject_once"},
	}
	if !reflect.DeepEqual(d.gotOptions, want) {
		t.Fatalf("options offered = %+v, want %+v", d.gotOptions, want)
	}
	if d.gotSummary != "run Bash" {
		t.Fatalf("summary = %q, want %q", d.gotSummary, "run Bash")
	}
	if d.gotCommand != "echo hi" {
		t.Fatalf("command = %q, want %q (from req.Input[\"command\"])", d.gotCommand, "echo hi")
	}
}

// TestClaudeDeciderAdapter_Deny proves a "deny" choice translates into
// allow=false, a nil updatedInput, and the fixed deny message -- the other
// half of the two-way translation TestClaudeDeciderAdapter_Allow covers.
func TestClaudeDeciderAdapter_Deny(t *testing.T) {
	t.Parallel()
	d := &recordingDecider{optionID: claudeOptionDeny}
	adapter := claudeDeciderAdapter{decider: d}

	req := claudecode.CanUseToolRequest{ToolName: "Bash", Input: map[string]any{"command": "rm -rf /"}}

	allow, updatedInput, denyMessage, updatedPermissions, interrupt, err := adapter.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if allow {
		t.Fatal("allow = true, want false")
	}
	if updatedInput != nil {
		t.Fatalf("updatedInput = %+v, want nil on deny", updatedInput)
	}
	if denyMessage != claudeFixedDenyMessage {
		t.Fatalf("denyMessage = %q, want %q", denyMessage, claudeFixedDenyMessage)
	}
	if updatedPermissions != nil || interrupt {
		t.Fatalf("updatedPermissions/interrupt = %+v/%v, want nil/false -- this pass never sets them", updatedPermissions, interrupt)
	}
	if d.gotCommand != "rm -rf /" {
		t.Fatalf("command = %q, want %q -- the decider must still see the real command even when it eventually denies", d.gotCommand, "rm -rf /")
	}
}

// TestBashCommand proves bashCommand only ever extracts a command for a
// Bash tool-use request, and never trusts a non-string "command" value --
// both cases where returning a wrong non-empty string would be a real
// safety issue, since a non-empty command is exactly what
// ApprovalPolicyAutoSafe's AllowlistedCommand check looks at.
func TestBashCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  claudecode.CanUseToolRequest
		want string
	}{
		{"bash with string command", claudecode.CanUseToolRequest{ToolName: "Bash", Input: map[string]any{"command": "go test ./..."}}, "go test ./..."},
		{"non-bash tool name", claudecode.CanUseToolRequest{ToolName: "Read", Input: map[string]any{"command": "go test ./..."}}, ""},
		{"bash with no input", claudecode.CanUseToolRequest{ToolName: "Bash"}, ""},
		{"bash with non-string command", claudecode.CanUseToolRequest{ToolName: "Bash", Input: map[string]any{"command": 123}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := bashCommand(tt.req); got != tt.want {
				t.Fatalf("bashCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaudeDeciderAdapter_PropagatesError(t *testing.T) {
	t.Parallel()
	wantErr := context.Canceled
	d := &recordingDecider{err: wantErr}
	adapter := claudeDeciderAdapter{decider: d}

	allow, updatedInput, denyMessage, updatedPermissions, interrupt, err := adapter.Decide(context.Background(), claudecode.CanUseToolRequest{})
	if err != wantErr {
		t.Fatalf("Decide() error = %v, want %v", err, wantErr)
	}
	if allow || updatedInput != nil || denyMessage != "" || updatedPermissions != nil || interrupt {
		t.Fatalf("on error want zero values, got allow=%v updatedInput=%+v denyMessage=%q updatedPermissions=%+v interrupt=%v",
			allow, updatedInput, denyMessage, updatedPermissions, interrupt)
	}
}
