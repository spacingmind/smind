package main

import (
	"bytes"
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

// waitCaptured runs `task wait <args>` into private buffers instead of
// swapping the os.Stdout/os.Stderr globals like sendCaptured. Use it for a wait
// that runs in a goroutine while the test body runs other commands: two
// concurrent global swaps race with each other and can leave os.Stdout pointing
// at a closed pipe.
func waitCaptured(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := runTaskWait(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
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

// TestCLINote_MatchesResolvedModel: the note names the model the daemon says
// the run launches with, and only for an unpinned claude-native send.
func TestCLINote_MatchesResolvedModel(t *testing.T) {
	got := unpinnedModelNote("claude-native", "", "claude-haiku-5-5")
	if !strings.Contains(got, "the run uses claude-haiku-5-5") || strings.Contains(got, "Opus") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("note = %q, want the resolved model named", got)
	}
	for _, tt := range []struct{ provider, requested, resolved string }{
		{"claude-native", "sonnet", "sonnet"},   // pinned
		{"claude-native", "default", "default"}, // the human's explicit choice
		{"claude-native", "", ""},               // nothing resolved: nothing to claim
		{"glm", "", "x"},                        // another provider
		{"codex-native", "", ""},
	} {
		if got := unpinnedModelNote(tt.provider, tt.requested, tt.resolved); got != "" {
			t.Errorf("unpinnedModelNote(%q, %q, %q) = %q, want none", tt.provider, tt.requested, tt.resolved, got)
		}
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
	if f := strings.Fields(lines[0]); f[4] != "MODEL" {
		t.Fatalf("header fields = %v, want MODEL fifth", f)
	}
	if f := strings.Fields(lines[1]); f[4] != "-" {
		t.Fatalf("row fields = %v, want model cell %q for an unpinned run", f, "-")
	}
}
