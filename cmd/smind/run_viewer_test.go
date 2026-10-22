package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestViewer(opts viewOptions, hold time.Duration) (*runViewer, *bytes.Buffer) {
	var buf bytes.Buffer
	v := newRunViewer("run-1", opts)
	v.w = &buf
	v.hold = hold
	return v, &buf
}

var rawAvailableCommands = rawEventParams{Kind: "available_commands_update", Payload: json.RawMessage(`{"sessionUpdate":"available_commands_update"}`)}

func TestRunViewer_RawHiddenByDefault(t *testing.T) {
	v, buf := newTestViewer(viewOptions{}, time.Hour)
	v.text("hello")
	v.raw(rawAvailableCommands)
	if got := buf.String(); got != "hello" {
		t.Fatalf("output = %q, want only the chunk text (raw hidden)", got)
	}
}

func TestRunViewer_RawVerbose(t *testing.T) {
	v, buf := newTestViewer(viewOptions{verbose: true}, time.Hour)
	v.raw(rawAvailableCommands)
	if got, want := buf.String(), renderRaw(rawAvailableCommands); got != want {
		t.Fatalf("verbose output = %q, want byte-identical %q", got, want)
	}
}

func TestRunViewer_AutoAcceptOneLine(t *testing.T) {
	v, buf := newTestViewer(viewOptions{autoAccept: true}, time.Hour)
	v.permissionRequest("req-1", "Run command: go test ./...", []permissionOptionParams{{ID: "allow", Label: "Allow", Kind: "allow_once"}}, &permissionDetail{Kind: "execute", Command: "go test ./..."})
	if buf.Len() != 0 {
		t.Fatalf("output before resolution = %q, want the request held", buf.String())
	}
	v.permissionResolved("req-1", "allow", "auto_accept")
	got := buf.String()
	if got != "[permission] auto-accepted Run command: go test ./... -> allow\n" {
		t.Fatalf("output = %q, want exactly one auto-accepted line", got)
	}
	if strings.Contains(got, "approve") {
		t.Fatalf("output %q still carries approve/deny hints", got)
	}
}

func TestRunViewer_AutoAcceptLineShowsCommandWhenSummaryLacksIt(t *testing.T) {
	// claude-native summarizes a Bash request as just "run Bash".
	got := renderAutoAccepted("run Bash", &permissionDetail{Kind: "execute", Command: "gofmt -l ."}, "allow")
	if got != "[permission] auto-accepted run Bash: gofmt -l . -> allow\n" {
		t.Fatalf("line = %q", got)
	}
	long := renderAutoAccepted("run Bash", &permissionDetail{Kind: "execute", Command: strings.Repeat("x", 500)}, "allow")
	if n := len([]rune(strings.TrimSuffix(long, "\n"))); n > len("[permission] auto-accepted ")+autoAcceptedMaxLen+len(" -> allow") || !strings.Contains(long, "…") {
		t.Fatalf("long command not capped: %d runes, %q", n, long)
	}
	if strings.Count(renderAutoAccepted("a\nb", nil, "allow"), "\n") != 1 {
		t.Fatal("a multi-line summary must stay on one line")
	}
}

// A request that does not resolve as an auto-accept within the hold is shown
// in full: a prompt that needs a human must never stay hidden.
func TestRunViewer_AutoAcceptHoldExpiresShowsBlock(t *testing.T) {
	v, buf := newTestViewer(viewOptions{autoAccept: true}, 30*time.Millisecond)
	v.permissionRequest("req-1", "Run command: rm x", nil, nil)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		v.mu.Lock()
		got := buf.String()
		v.mu.Unlock()
		if strings.Contains(got, "-> approve: smind task approve run-1 req-1") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("held request never printed in full, output = %q", buf.String())
}

func TestRunViewer_HeldRequestResolvedByHumanPrintsBlockAndResolution(t *testing.T) {
	v, buf := newTestViewer(viewOptions{autoAccept: true}, time.Hour)
	v.permissionRequest("req-1", "Run command: ls", nil, nil)
	v.permissionResolved("req-1", "deny", "human")
	got := buf.String()
	if !strings.Contains(got, "-> approve:") || !strings.Contains(got, "[permission] req-1 -> deny (human)") || strings.Contains(got, "auto-accepted") {
		t.Fatalf("output = %q, want the block then the human resolution", got)
	}
}

func TestRunViewer_FlushPrintsStillHeldRequest(t *testing.T) {
	v, buf := newTestViewer(viewOptions{autoAccept: true}, time.Hour)
	v.permissionRequest("req-1", "Run command: ls", nil, nil)
	v.flush()
	if !strings.Contains(buf.String(), "-> approve:") {
		t.Fatalf("flush output = %q, want the held block", buf.String())
	}
	v.flush() // idempotent
	if strings.Count(buf.String(), "-> approve:") != 1 {
		t.Fatalf("flush printed the block twice: %q", buf.String())
	}
}

func TestRunViewer_ManualRunUnchanged(t *testing.T) {
	v, buf := newTestViewer(viewOptions{}, time.Hour)
	v.permissionRequest("req-1", "Run command: ls", nil, nil)
	if got, want := buf.String(), renderPermissionRequest("run-1", "req-1", "Run command: ls", nil, nil); got != want {
		t.Fatalf("manual request = %q, want the full block immediately %q", got, want)
	}
	v.permissionResolved("req-1", "allow", "human")
	if !strings.HasSuffix(buf.String(), renderPermissionResolved("req-1", "allow", "human")) {
		t.Fatalf("manual resolution missing: %q", buf.String())
	}
}

// printRunLogs derives the collapse from the listing itself: an auto_accept
// resolution folds its request into one line, with no flag needed.
func TestPrintRunLogs_AutoAcceptCollapsesAndHidesRaw(t *testing.T) {
	result := runLogsResult{RunID: "run-9", Status: "done", StopReason: "end_turn", Events: []runLogEvent{
		{Type: "raw", rawEventParams: rawAvailableCommands},
		{Type: "permission_request", RequestID: "q1", Summary: "Run command: ls", Options: []permissionOptionParams{{ID: "allow", Label: "Allow", Kind: "allow_once"}}},
		{Type: "permission_resolved", RequestID: "q1", OptionID: "allow", Reason: "auto_accept"},
		{Type: "permission_request", RequestID: "q2", Summary: "Run command: rm x"},
		{Type: "permission_resolved", RequestID: "q2", OptionID: "deny", Reason: "human"},
	}}
	out := captureStdout(t, func() { printRunLogs(result, viewOptions{}) })
	if strings.Contains(out, "[raw]") {
		t.Fatalf("logs output %q shows a raw event without --verbose", out)
	}
	if !strings.Contains(out, "[permission] auto-accepted Run command: ls -> allow\n") {
		t.Fatalf("logs output %q lacks the one-line auto-accept", out)
	}
	if strings.Contains(out, "request q1") || strings.Contains(out, "q1 -> allow") {
		t.Fatalf("logs output %q still prints q1's block or resolution", out)
	}
	// The human-decided request keeps its full block and resolution.
	if !strings.Contains(out, "request q2") || !strings.Contains(out, "[permission] q2 -> deny (human)") {
		t.Fatalf("logs output %q lost the human-decided request", out)
	}
	verbose := captureStdout(t, func() { printRunLogs(result, viewOptions{verbose: true}) })
	if !strings.Contains(verbose, "[raw] \"available_commands_update\"") {
		t.Fatalf("--verbose logs output %q lacks the raw event", verbose)
	}
}

// End to end against a real daemon and the fake ACP agent's "permission"
// scenario: `task send --auto-accept` prints exactly one permission line, and
// `task logs` / `task attach` on the finished run print the same one.
func TestTaskSend_AutoAcceptPrintsOnePermissionLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	var task struct {
		WorktreePath *string `json:"WorktreePath"`
	}
	if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
		t.Fatalf("task.get: %v", err)
	}
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("permission"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	id := strconv.FormatInt(taskID, 10)

	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "go", "--auto-accept"})
	if code != 0 {
		t.Fatalf("send: exit %d stderr %q", code, stderr)
	}
	if n := strings.Count(stdout, "[permission]"); n != 1 || !strings.Contains(stdout, "auto-accepted") || strings.Contains(stdout, "approve:") {
		t.Fatalf("send --auto-accept stdout = %q, want exactly one auto-accepted permission line (got %d)", stdout, n)
	}

	var runID string
	for _, f := range strings.Fields(stderr) {
		if len(f) == 32 {
			runID = f
			break
		}
	}
	if runID == "" {
		t.Fatalf("no run id in stderr %q", stderr)
	}
	for _, args := range [][]string{{"task", "logs", runID}, {"task", "attach", runID}} {
		code, out, errOut := sendCaptured(t, args)
		if code != 0 {
			t.Fatalf("%v: exit %d stderr %q", args, code, errOut)
		}
		if n := strings.Count(out, "[permission]"); n != 1 || !strings.Contains(out, "auto-accepted") || strings.Contains(out, "approve:") {
			t.Fatalf("%v stdout = %q, want exactly one auto-accepted line (got %d)", args, out, n)
		}
		if strings.Contains(out, "[raw]") {
			t.Fatalf("%v stdout = %q shows a raw event without --verbose", args, out)
		}
	}
}

func TestTaskAttachLogs_VerboseFlagAndUsage(t *testing.T) {
	for _, args := range [][]string{{"task", "attach"}, {"task", "attach", "--bogus", "r"}, {"task", "attach", "a", "b"}} {
		if code, _, stderr := sendCaptured(t, args); code != 2 || !strings.Contains(stderr, "usage: smind task attach <runId> [--verbose]") {
			t.Errorf("%v: code %d stderr %q, want usage exit 2", args, code, stderr)
		}
	}
	if code, _, stderr := sendCaptured(t, []string{"task", "logs", "--bogus"}); code != 2 || !strings.Contains(stderr, "[--verbose]") {
		t.Errorf("task logs --bogus: code %d stderr %q, want usage mentioning --verbose", code, stderr)
	}
}
