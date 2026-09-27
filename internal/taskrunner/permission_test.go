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
	gotOptions []PermissionOption
}

func (d *recordingDecider) Decide(_ context.Context, summary string, options []PermissionOption) (string, error) {
	d.gotSummary = summary
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
