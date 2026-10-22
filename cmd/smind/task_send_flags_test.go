package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/wsclient"
)

// hangingTask sets up the test env with a task whose runs never finish.
func hangingTask(t *testing.T) (*wsclient.Client, string) {
	t.Helper()
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
	if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte("hang"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}
	return client, strconv.FormatInt(taskID, 10)
}

func stopRun(t *testing.T, client *wsclient.Client, runID string) {
	t.Helper()
	_ = client.Call(context.Background(), "run.stop", map[string]any{"runId": runID}, nil)
}

func listRuns(t *testing.T, client *wsclient.Client) []runSummaryRow {
	t.Helper()
	var rows []runSummaryRow
	if err := client.Call(context.Background(), "run.list", map[string]any{}, &rows); err != nil {
		t.Fatalf("run.list: %v", err)
	}
	return rows
}

// sendCaptured runs the CLI and returns exit code, stdout, stderr.
func sendCaptured(t *testing.T, args []string) (int, string, string) {
	t.Helper()
	code := -1
	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() { code = run(args) })
	})
	return code, stdout, stderr
}

func TestTaskSendDetachPrintsRunIDAndSkipsAttach(t *testing.T) {
	client, id := hangingTask(t)
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi", "--detach"})
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", code, stderr)
	}
	runID := strings.TrimSpace(stdout)
	if runID == "" || strings.ContainsAny(runID, " \t\n") {
		t.Fatalf("stdout = %q, want just the run id", stdout)
	}
	if !strings.Contains(stderr, "started") {
		t.Fatalf("stderr = %q, want it to contain started", stderr)
	}
	defer stopRun(t, client, runID)
	waitForRunStatus(t, client, runID, "running")
	found := false
	for _, r := range listRuns(t, client) {
		if r.ID == runID {
			found = true
			if r.Status != "running" {
				t.Fatalf("status = %q, want running", r.Status)
			}
		}
	}
	if !found {
		t.Fatalf("run %s not in run.list", runID)
	}
}

func TestTaskSendThinkingFlag(t *testing.T) {
	client, id := hangingTask(t)

	code, _, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi", "--thinking", "bogus"})
	if code != 2 || !strings.Contains(stderr, "invalid --thinking") || !strings.Contains(stderr, "off, standard, or extended") {
		t.Fatalf("bogus: code %d stderr %q", code, stderr)
	}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--thinking", "extended", "--detach"}, "extended"},
		{[]string{"--thinking=off", "--detach"}, "off"},
	}
	for _, tc := range cases {
		code, stdout, stderr := sendCaptured(t, append([]string{"task", "send", id, "glm", "hi"}, tc.args...))
		if code != 0 {
			t.Fatalf("%v: exit %d stderr %q", tc.args, code, stderr)
		}
		runID := strings.TrimSpace(stdout)
		waitForRunStatus(t, client, runID, "running")
		got := ""
		for _, r := range listRuns(t, client) {
			if r.ID == runID {
				got = r.ThinkingLevel
			}
		}
		if got != tc.want {
			t.Fatalf("%v: ThinkingLevel = %q, want %q", tc.args, got, tc.want)
		}
		// Free the chat for the next case (a busy chat rejects a plain send).
		stopRun(t, client, runID)
		waitForRunStatus(t, client, runID, "stopped")
	}
}

func TestUnpinnedModelNote(t *testing.T) {
	tests := []struct {
		provider, model string
		wantNote        bool
	}{
		{"claude-native", "", true},
		{"claude-native", "sonnet", false},
		{"claude-native", "default", false},
		{"glm", "", false},
		{"codex-native", "", false},
	}
	for _, tt := range tests {
		got := unpinnedModelNote(tt.provider, tt.model, "")
		if (got != "") != tt.wantNote {
			t.Errorf("unpinnedModelNote(%q, %q) = %q, want note=%v", tt.provider, tt.model, got, tt.wantNote)
		}
		if got != "" && (!strings.Contains(got, "Claude CLI's own default") || !strings.HasSuffix(got, "\n")) {
			t.Errorf("unpinnedModelNote note = %q, want the default-model wording and a trailing newline", got)
		}
	}
}

// TestUnpinnedModelNote_ConfiguredDefault: with a configured default the note
// names the model instead of warning about the CLI's own default.
func TestUnpinnedModelNote_ConfiguredDefault(t *testing.T) {
	got := unpinnedModelNote("claude-native", "", "sonnet")
	if !strings.Contains(got, "configured default model sonnet") || strings.Contains(got, "may be Opus") {
		t.Fatalf("note = %q, want the configured model named and no Opus warning", got)
	}
	if got := unpinnedModelNote("claude-native", "haiku", "sonnet"); got != "" {
		t.Fatalf("pinned run note = %q, want none", got)
	}
	if got := unpinnedModelNote("glm", "", "x"); got != "" {
		t.Fatalf("glm note = %q, want none", got)
	}
}

func TestRunModelCell(t *testing.T) {
	reported := "claude-opus-5-5"
	usage := &struct {
		Model        *string  `json:"model"`
		InputTokens  *int64   `json:"inputTokens"`
		OutputTokens *int64   `json:"outputTokens"`
		CostUSD      *float64 `json:"costUsd"`
	}{Model: &reported}
	tests := []struct {
		name string
		row  runSummaryRow
		want string
	}{
		{"requested", runSummaryRow{Model: "sonnet"}, "sonnet"},
		{"reported only, in parens", runSummaryRow{Usage: usage}, "(claude-opus-5-5)"},
		{"requested wins over reported", runSummaryRow{Model: "sonnet", Usage: usage}, "sonnet"},
		{"neither", runSummaryRow{}, "-"},
	}
	for _, tt := range tests {
		if got := runModelCell(tt.row); got != tt.want {
			t.Errorf("%s: runModelCell = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTaskRunsShowsModel(t *testing.T) {
	client, id := hangingTask(t)
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", id, "glm", "hi", "--detach"})
	if code != 0 {
		t.Fatalf("send: exit %d stderr %q", code, stderr)
	}
	runID := strings.TrimSpace(stdout)
	defer stopRun(t, client, runID)
	waitForRunStatus(t, client, runID, "running")

	code, out, stderr := sendCaptured(t, []string{"task", "runs", id})
	if code != 0 {
		t.Fatalf("task runs: exit %d stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "MODEL") || !strings.HasPrefix(lines[1], runID) {
		t.Fatalf("task runs output = %q, want a MODEL header and the run row", out)
	}
	if f := strings.Fields(lines[0]); f[3] != "MODEL" {
		t.Fatalf("header fields = %v, want MODEL fourth", f)
	}
	if f := strings.Fields(lines[1]); f[3] != "-" {
		t.Fatalf("row fields = %v, want model cell %q for an unpinned run", f, "-")
	}
}
