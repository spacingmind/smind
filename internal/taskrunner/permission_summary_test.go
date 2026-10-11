package taskrunner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	claudecode "github.com/spacingmind/claude-agent-sdk-go"
)

func TestPermissionSummary_WriteShowsLineCountChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// ACP, rawInput (old content read from disk, relative path under cwd).
	raw := json.RawMessage(`{"toolCallId":"t","title":"Write file a.txt","kind":"edit","rawInput":{"file_path":"a.txt","content":"1\n2\n3\n4\n5\n"}}`)
	got := summarizeACPToolCallIn(dir, raw)
	for _, want := range []string{"Write file a.txt", "overwrites existing file", "3 -> 5 lines", "10 bytes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ACP summary = %q, want %q", got, want)
		}
	}

	// ACP diff block carries the old text itself.
	diff := json.RawMessage(`{"toolCallId":"t","title":"Edit","content":[{"type":"diff","path":"/nowhere/x","oldText":"a\nb\n","newText":"a\n"}]}`)
	if got := summarizeACPToolCallIn("", diff); !strings.Contains(got, "overwrites existing file: 2 -> 1 lines, 2 bytes") {
		t.Fatalf("diff summary = %q", got)
	}

	// Claude native Write.
	d := &recordingDecider{optionID: claudeOptionAllow}
	_, _, _, _, _, err := claudeDeciderAdapter{decider: d, cwd: dir}.Decide(t.Context(), claudecode.CanUseToolRequest{
		ToolName: "Write", Input: map[string]any{"file_path": "a.txt", "content": "only\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.gotSummary, "3 -> 1 lines") || !strings.Contains(d.gotSummary, "overwrites existing file") {
		t.Fatalf("claude summary = %q", d.gotSummary)
	}
}

func TestPermissionSummary_WriteNewFileSize(t *testing.T) {
	dir := t.TempDir()
	raw := json.RawMessage(`{"toolCallId":"t","title":"Write file new.txt","rawInput":{"path":"new.txt","content":"hello\nworld\n"}}`)
	got := summarizeACPToolCallIn(dir, raw)
	if !strings.Contains(got, "new file, 12 bytes, 2 lines") || strings.Contains(got, "overwrites") {
		t.Fatalf("summary = %q, want a new-file size note", got)
	}
	// Non-write tool calls are unchanged.
	if got := summarizeACPToolCallIn(dir, json.RawMessage(`{"title":"Run ls"}`)); got != "Run ls" {
		t.Fatalf("summary = %q, want the bare title", got)
	}
}
