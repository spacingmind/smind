package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/wsclient"
)

func TestWaitExitCode(t *testing.T) {
	tests := []struct {
		row  runSummaryRow
		want int
	}{
		{runSummaryRow{Status: "done"}, 0},
		{runSummaryRow{Status: "done", Disposition: "needs-continue"}, 4},
		{runSummaryRow{Status: "error"}, 1},
		{runSummaryRow{Status: "stopped"}, 3},
		{runSummaryRow{Status: "running"}, 1},
	}
	for _, tc := range tests {
		if got := waitExitCode(tc.row); got != tc.want {
			t.Errorf("waitExitCode(%+v) = %d, want %d", tc.row, got, tc.want)
		}
	}
}

func TestRenderWaitSummary_NamesTask(t *testing.T) {
	got := renderWaitSummary(runSummaryRow{ID: "abc", TaskID: 12, Status: "done"}, "", time.Now())
	if !strings.HasPrefix(got, "run abc (task #12) done after") {
		t.Errorf("summary %q does not name the task", got)
	}
}

func TestRenderWaitSummary_UnknownTaskKeepsOldText(t *testing.T) {
	got := renderWaitSummary(runSummaryRow{ID: "abc", Status: "done"}, "", time.Now())
	if !strings.HasPrefix(got, "run abc done after") || strings.Contains(got, "task #") {
		t.Errorf("summary %q should keep the old text without a task", got)
	}
}

func TestRenderWaitSummary(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	fin := start.Add(192 * time.Second)
	in, out, cost := int64(1200), int64(340), 0.1234
	head, base := "1234567890", "7654321000"
	row := runSummaryRow{ID: "abc", Status: "done", StopReason: "end_turn", Model: "sonnet", StartedAt: start, FinishedAt: &fin, BaseSHA: &base, HeadSHA: &head}
	row.Usage = &struct {
		Model        *string  `json:"model"`
		InputTokens  *int64   `json:"inputTokens"`
		OutputTokens *int64   `json:"outputTokens"`
		CostUSD      *float64 `json:"costUsd"`
	}{InputTokens: &in, OutputTokens: &out, CostUSD: &cost}
	got := renderWaitSummary(row, "all done", fin)
	for _, w := range []string{"run abc done (end_turn) after 3m12s", "model sonnet", "1200 in / 340 out tokens / $0.12", "head 1234567 (base 7654321)", "\n\nall done"} {
		if !strings.Contains(got, w) {
			t.Errorf("summary %q missing %q", got, w)
		}
	}

	bare := renderWaitSummary(runSummaryRow{ID: "x", Status: "done", StartedAt: start}, "", start.Add(850*time.Millisecond))
	for _, w := range []string{"model default", "no usage reported", "git state unknown", "after 850ms"} {
		if !strings.Contains(bare, w) {
			t.Errorf("bare summary %q missing %q", bare, w)
		}
	}
	if strings.Contains(bare, "\n") {
		t.Errorf("empty finalText should add nothing: %q", bare)
	}

	same := "aaaaaaaaaa"
	eq := renderWaitSummary(runSummaryRow{ID: "x", Status: "done", BaseSHA: &same, HeadSHA: &same}, "", time.Now())
	if !strings.Contains(eq, "no new commits") {
		t.Errorf("equal SHAs: %q", eq)
	}

	nc := renderWaitSummary(runSummaryRow{ID: "x", Status: "done", Disposition: "needs-continue", StopReason: "max_tokens"}, "", time.Now())
	if !strings.Contains(nc, "done (needs-continue) (max_tokens)") {
		t.Errorf("needs-continue: %q", nc)
	}

	er := renderWaitSummary(runSummaryRow{ID: "x", Status: "error", Err: "boom"}, "text", time.Now())
	if lines := strings.Split(er, "\n"); lines[len(lines)-1] != "error: boom" {
		t.Errorf("error row last line: %q", er)
	}

	long := renderWaitSummary(runSummaryRow{ID: "x", Status: "done"}, strings.Repeat("é", 1000), time.Now())
	body := long[strings.Index(long, "\n\n")+2:]
	if r := []rune(body); len(r) != 601 || r[600] != '…' {
		t.Errorf("long text: got %d runes", len(r))
	}
	collapsed := renderWaitSummary(runSummaryRow{ID: "x", Status: "done"}, "a\n\n\n\n\nb", time.Now())
	if !strings.HasSuffix(collapsed, "a\n\nb") {
		t.Errorf("newlines not collapsed: %q", collapsed)
	}

	for d, want := range map[time.Duration]string{
		850 * time.Millisecond:                    "850ms",
		12 * time.Second:                          "12s",
		192 * time.Second:                         "3m12s",
		time.Hour + 2*time.Minute + 5*time.Second: "1h02m05s",
	} {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

// waitEnv starts a daemon with one task whose scenario file is scenario
// ("" = leave the default reply scenario).
func waitEnv(t *testing.T, scenario string) (*wsclient.Client, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnv(t, home)
	writeTestConfig(t, home, srvURL)
	client := dialTestClient(t, srvURL)
	taskID := newTestRepoTask(t, client)
	if scenario != "" {
		var task struct {
			WorktreePath *string `json:"WorktreePath"`
		}
		if err := client.Call(context.Background(), "task.get", map[string]any{"id": taskID}, &task); err != nil {
			t.Fatalf("task.get: %v", err)
		}
		if err := os.WriteFile(filepath.Join(*task.WorktreePath, "scenario"), []byte(scenario), 0o644); err != nil {
			t.Fatalf("write scenario: %v", err)
		}
	}
	return client, strconv.FormatInt(taskID, 10)
}

func detachedRun(t *testing.T, taskID string) string {
	t.Helper()
	code, stdout, stderr := sendCaptured(t, []string{"task", "send", taskID, "glm", "hi", "--detach"})
	if code != 0 {
		t.Fatalf("task send --detach = %d; stderr = %q", code, stderr)
	}
	return strings.TrimSpace(stdout)
}

func TestTaskWait_ExitCodes(t *testing.T) {
	t.Run("done", func(t *testing.T) {
		_, taskID := waitEnv(t, "")
		runID := detachedRun(t, taskID)
		code, stdout, stderr := sendCaptured(t, []string{"task", "wait", runID})
		if code != 0 || !strings.Contains(stdout, "done (end_turn)") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
	})
	t.Run("needs-continue", func(t *testing.T) {
		_, taskID := waitEnv(t, "stop max_turn_requests")
		runID := detachedRun(t, taskID)
		code, stdout, stderr := sendCaptured(t, []string{"task", "wait", runID})
		if code != 4 || !strings.Contains(stdout, "needs-continue") {
			t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
		}
	})
	t.Run("stopped", func(t *testing.T) {
		client, taskID := waitEnv(t, "hang")
		runID := detachedRun(t, taskID)
		waitForRunStatus(t, client, runID, "running")
		type res struct {
			code           int
			stdout, stderr string
		}
		ch := make(chan res, 1)
		go func() {
			c, o, e := waitCaptured(runID)
			ch <- res{c, o, e}
		}()
		time.Sleep(200 * time.Millisecond)
		stopRun(t, client, runID)
		select {
		case r := <-ch:
			if r.code != 3 || !strings.Contains(r.stdout, "stopped") {
				t.Fatalf("code = %d, stdout = %q, stderr = %q", r.code, r.stdout, r.stderr)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("wait did not return after run.stop")
		}
	})
	t.Run("timeout", func(t *testing.T) {
		client, taskID := waitEnv(t, "hang")
		runID := detachedRun(t, taskID)
		defer stopRun(t, client, runID)
		waitForRunStatus(t, client, runID, "running")
		code, _, stderr := sendCaptured(t, []string{"task", "wait", runID, "--timeout", "300ms"})
		if code != 5 || !strings.Contains(stderr, "timed out after 300ms") {
			t.Fatalf("code = %d, stderr = %q", code, stderr)
		}
		waitForRunStatus(t, client, runID, "running")
	})
	// No error-status scenario exists in the fake agent, so exit 1 for a
	// run that ended in error is covered only by TestWaitExitCode.
}

func TestTaskWait_AlreadyFinished(t *testing.T) {
	client, taskID := waitEnv(t, "")
	runID := detachedRun(t, taskID)
	waitForRunStatus(t, client, runID, "done")
	start := time.Now()
	code, stdout, stderr := sendCaptured(t, []string{"task", "wait", runID})
	if code != 0 || !strings.Contains(stdout, "done (end_turn)") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("wait on a finished run took %v", d)
	}
}

func TestTaskWait_UnknownRun(t *testing.T) {
	waitEnv(t, "")
	code, _, stderr := sendCaptured(t, []string{"task", "wait", "deadbeef"})
	if code != 1 || !strings.Contains(stderr, "task wait:") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestTaskWait_Usage(t *testing.T) {
	for _, args := range [][]string{
		{"task", "wait"},
		{"task", "wait", "abc", "--timeout", "x"},
		{"task", "wait", "abc", "def"},
	} {
		code, _, stderr := sendCaptured(t, args)
		if code != 2 || !strings.Contains(stderr, cmdTaskWaitUsage) {
			t.Errorf("%v: code = %d, stderr = %q", args, code, stderr)
		}
	}
}

func TestTaskWait_PrintsPendingPermissionHint(t *testing.T) {
	client, taskID := waitEnv(t, "permission")
	runID := detachedRun(t, taskID)

	var mu sync.Mutex
	var stderrBuf string
	type res struct{ code int }
	ch := make(chan res, 1)
	go func() {
		code := -1
		var e string
		code, _, e = waitCaptured(runID)
		mu.Lock()
		stderrBuf = e
		mu.Unlock()
		ch <- res{code}
	}()

	var reqID string
	deadline := time.Now().Add(10 * time.Second)
	for reqID == "" {
		pending, err := fetchPendingPermissions(context.Background(), client, runID)
		if err == nil && len(pending) > 0 {
			reqID = pending[0].RequestID
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending permission appeared")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Give wait's announcer goroutine time to see the request before approving.
	time.Sleep(500 * time.Millisecond)
	if code := run([]string{"task", "approve", runID}); code != 0 {
		t.Fatalf("task approve = %d", code)
	}
	select {
	case r := <-ch:
		if r.code != 0 {
			t.Fatalf("wait exit = %d", r.code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wait did not finish after approve")
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "smind task approve " + runID + " " + reqID; !strings.Contains(stderrBuf, want) {
		t.Fatalf("stderr = %q, want it to contain %q", stderrBuf, want)
	}
}

// TestTaskWait_ErrorRun is the live-run half of the error exit: a run that
// fails like GLM on a usage limit (an "[error] ..." chunk, then a generic
// JSON-RPC error) makes `task wait` exit 6 (rate limited, ADR-0041) and show
// the agent's own cause; `task runs` shows the rate-limit text in its STATUS
// cell instead (the full cause stays in wait/logs).
func TestTaskWait_ErrorRun(t *testing.T) {
	_, taskID := waitEnv(t, "quota-error")
	runID := detachedRun(t, taskID)

	code, stdout, stderr := sendCaptured(t, []string{"task", "wait", runID})
	if code != 6 {
		t.Fatalf("exit = %d, want 6; stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{
		"error after",
		"error: 429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18",
		"rpc error -32603: Internal error",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("wait stdout %q missing %q", stdout, want)
		}
	}

	code, out, stderr := sendCaptured(t, []string{"task", "runs", taskID})
	if code != 0 || !strings.Contains(out, "error: rate limited (5h window)") {
		t.Fatalf("task runs: exit %d, out %q, stderr %q, want the rate-limit text in the STATUS cell", code, out, stderr)
	}
}

// TestTaskWait_ExitSixOnRateLimited: a run that hit a provider quota exits 6
// and prints the rate-limit line right after the outcome line.
func TestTaskWait_ExitSixOnRateLimited(t *testing.T) {
	_, taskID := waitEnv(t, "quota-error")
	runID := detachedRun(t, taskID)

	code, stdout, stderr := sendCaptured(t, []string{"task", "wait", runID})
	if code != 6 {
		t.Fatalf("exit = %d, want 6; stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "rate limited (5h window)") {
		t.Fatalf("stdout = %q, want the rate-limit line", stdout)
	}
	lines := strings.Split(stdout, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[1], "rate limited (5h window)") {
		t.Fatalf("stdout = %q, want the rate-limit line second", stdout)
	}
}

// TestTaskWait_OtherExitCodesUnchanged pins waitExitCode for every
// non-rate-limited outcome.
func TestTaskWait_OtherExitCodesUnchanged(t *testing.T) {
	tests := []struct {
		name string
		row  runSummaryRow
		want int
	}{
		{"done", runSummaryRow{Status: "done"}, 0},
		{"done needs-continue", runSummaryRow{Status: "done", Disposition: "needs-continue"}, 4},
		{"stopped", runSummaryRow{Status: "stopped"}, 3},
		{"error", runSummaryRow{Status: "error"}, 1},
		{"error other kind", runSummaryRow{Status: "error", FailureKind: "other"}, 1},
		{"error rate limited", runSummaryRow{Status: "error", FailureKind: "rate_limited"}, 6},
	}
	for _, tt := range tests {
		if got := waitExitCode(tt.row); got != tt.want {
			t.Errorf("%s: waitExitCode = %d, want %d", tt.name, got, tt.want)
		}
	}
}

// TestRenderWaitSummary_ErrorRunDoesNotRepeatTheAgentErrorLine: the agent's
// "[error] ..." closing message is not printed as the final text of a failed
// run, since the error line already says it; other final text is kept.
func TestRenderWaitSummary_ErrorRunDoesNotRepeatTheAgentErrorLine(t *testing.T) {
	row := runSummaryRow{ID: "r", Status: "error", Err: "429 Usage limit reached (taskrunner: ...)", StartedAt: time.Now()}
	got := renderWaitSummary(row, "\n\n[error] 429 Usage limit reached", time.Now())
	if strings.Count(got, "429 Usage limit reached") != 1 || !strings.Contains(got, "\nerror: 429 Usage limit reached") {
		t.Fatalf("summary = %q, want the cause once, on the error line", got)
	}
	if kept := renderWaitSummary(row, "I could not finish: the build failed", time.Now()); !strings.Contains(kept, "I could not finish: the build failed") {
		t.Fatalf("summary = %q, want ordinary final text of a failed run kept", kept)
	}
	done := runSummaryRow{ID: "r", Status: "done", StartedAt: time.Now()}
	if got := renderWaitSummary(done, "[error] is the literal output I was asked for", time.Now()); !strings.Contains(got, "[error] is the literal output") {
		t.Fatalf("summary = %q, want a done run's text untouched", got)
	}
}
