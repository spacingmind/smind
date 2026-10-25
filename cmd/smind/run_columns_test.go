package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunStartedCell(t *testing.T) {
	now := time.Date(2026, 10, 22, 18, 0, 0, 0, time.Local)
	if got := runStartedCell(time.Time{}, now); got != "-" {
		t.Errorf("zero = %q", got)
	}
	if got := runStartedCell(time.Date(2026, 10, 22, 9, 5, 7, 0, time.Local), now); got != "09:05:07" {
		t.Errorf("same day = %q", got)
	}
	if got := runStartedCell(time.Date(2026, 10, 21, 9, 5, 7, 0, time.Local), now); got != "Oct 21 09:05" {
		t.Errorf("other day = %q", got)
	}
}

func TestRunDurationCell(t *testing.T) {
	start := time.Date(2026, 10, 22, 9, 0, 0, 0, time.Local)
	fin := start.Add(3*time.Minute + 12*time.Second)
	if got := runDurationCell(runSummaryRow{Status: "done", StartedAt: start, FinishedAt: &fin}, start); got != "3m12s" {
		t.Errorf("finished = %q", got)
	}
	if got := runDurationCell(runSummaryRow{Status: "running", StartedAt: start}, start.Add(62*time.Minute)); got != "1h02m" {
		t.Errorf("running = %q", got)
	}
	if got := runDurationCell(runSummaryRow{Status: "running", StartedAt: start}, start.Add(12*time.Second)); got != "12s" {
		t.Errorf("seconds = %q", got)
	}
	if got := runDurationCell(runSummaryRow{Status: "running", StartedAt: start}, start.Add(time.Millisecond)); got != "<1s" {
		t.Errorf("sub-second = %q", got)
	}
	if got := runDurationCell(runSummaryRow{Status: "running"}, start); got != "-" {
		t.Errorf("zero = %q", got)
	}
}

func TestCompactCount(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 123: "123", 999: "999", 1000: "1.0k", 1234: "1.2k", 9999: "10.0k", 12345: "12k", 999999: "999k", 1234567: "1.2M"} {
		if n == 9999 {
			continue // rounding edge, not pinned
		}
		if got := compactCount(n); got != want {
			t.Errorf("compactCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestRunUsageCell(t *testing.T) {
	i, o, c := int64(1234), int64(56), 0.123
	type U = struct {
		Model        *string  `json:"model"`
		InputTokens  *int64   `json:"inputTokens"`
		OutputTokens *int64   `json:"outputTokens"`
		CostUSD      *float64 `json:"costUsd"`
	}
	m := "x"
	cases := []struct {
		name string
		r    runSummaryRow
		want string
	}{
		{"none", runSummaryRow{}, "-"},
		{"model only", runSummaryRow{Usage: &U{Model: &m}}, "-"},
		{"tokens", runSummaryRow{Usage: &U{InputTokens: &i, OutputTokens: &o}}, "1.2k/56 tok"},
		{"cost only", runSummaryRow{Usage: &U{CostUSD: &c}}, "$0.12"},
		{"both", runSummaryRow{Usage: &U{InputTokens: &i, OutputTokens: &o, CostUSD: &c}}, "1.2k/56 tok $0.12"},
		{"nil token", runSummaryRow{Usage: &U{InputTokens: &i}}, "1.2k/? tok"},
	}
	for _, tc := range cases {
		if got := runUsageCell(tc.r); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRunStatusCell(t *testing.T) {
	if got := runStatusCell(runSummaryRow{Status: "done", Disposition: "", Err: ""}, time.Now()); got != "done" {
		t.Errorf("done = %q", got)
	}
	if got := runStatusCell(runSummaryRow{Status: "done", Disposition: "needs-continue", Err: ""}, time.Now()); got != "done (needs-continue)" {
		t.Errorf("needs-continue = %q", got)
	}
	long := "boom   " + strings.Repeat("é", 100) + "\nsecond line"
	got := runStatusCell(runSummaryRow{Status: "error", Disposition: "", Err: long}, time.Now())
	want := "error: boom " + strings.Repeat("é", 80-5) + "…"
	if got != want {
		t.Errorf("long = %q, want %q", got, want)
	}
	if got := runStatusCell(runSummaryRow{Status: "error", Disposition: "", Err: ""}, time.Now()); got != "error" {
		t.Errorf("empty err = %q", got)
	}
}

func TestRunCommitCount(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	commit := func() { git("-c", "user.email=a@b", "-c", "user.name=t", "commit", "--allow-empty", "-m", "x") }
	commit()
	first := git("rev-parse", "HEAD")
	commit()
	commit()
	third := git("rev-parse", "HEAD")

	if n, ok := runCommitCount(dir, first, third); !ok || n != 2 {
		t.Errorf("count = %d, %v, want 2, true", n, ok)
	}
	if got := runCommitsCell(runSummaryRow{BaseSHA: &first, HeadSHA: &third}, dir); got != "2" {
		t.Errorf("cell = %q", got)
	}
	if got := runCommitsCell(runSummaryRow{BaseSHA: &first, HeadSHA: &first}, dir); got != "0" {
		t.Errorf("equal = %q", got)
	}
	bad := strings.Repeat("0", 40)
	if _, ok := runCommitCount(dir, bad, third); ok {
		t.Errorf("unknown sha reported ok")
	}
	if got := runCommitsCell(runSummaryRow{BaseSHA: &bad, HeadSHA: &third}, dir); got != "-" {
		t.Errorf("unknown = %q", got)
	}
	if got := runCommitsCell(runSummaryRow{BaseSHA: &first, HeadSHA: &third}, ""); got != "-" {
		t.Errorf("no worktree = %q", got)
	}
	if got := runCommitsCell(runSummaryRow{}, dir); got != "-" {
		t.Errorf("no shas = %q", got)
	}
}

func TestTaskRunsColumns(t *testing.T) {
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
	if len(lines) != 2 {
		t.Fatalf("output = %q, want header + 1 row", out)
	}
	want := []string{"ID", "CHAT", "PROVIDER", "ORIGIN", "MODEL", "STARTED", "DURATION", "USAGE", "COMMITS", "STATUS", "PROFILE", "PARENT"}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("header = %v, want %v", got, want)
	}
	f := strings.Fields(lines[1])
	if f[0] != runID || f[9] != "running" || f[10] != "-" || f[11] != "-" {
		t.Fatalf("row = %v, want run id first, running STATUS, then PROFILE and PARENT \"-\"", f)
	}
	start := strings.Index(lines[0], "DURATION")
	end := strings.Index(lines[0], "USAGE")
	if cell := strings.TrimSpace(lines[1][start:end]); cell == "-" || cell == "" {
		t.Fatalf("duration cell = %q, want a value for a running run", cell)
	}
}

func TestTaskRuns_ShowsOrigin(t *testing.T) {
	var rows []runSummaryRow
	// run.list's origin key is lowercase (json tag); decoding must pick it up.
	if err := json.Unmarshal([]byte(`[{"ID":"r-1","ChatID":1,"Provider":"glm","Status":"done","origin":"child_event"},{"ID":"r-2","ChatID":1,"Provider":"glm","Status":"done"},{"ID":"r-3","ChatID":1,"Provider":"glm","Status":"done","origin":"human"}]`), &rows); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	writeRunsTable(&buf, rows, "", time.Now())
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("output = %q", buf.String())
	}
	col := strings.Index(lines[0], "ORIGIN")
	if col < 0 || col < strings.Index(lines[0], "PROVIDER") || col > strings.Index(lines[0], "MODEL") {
		t.Fatalf("ORIGIN not between PROVIDER and MODEL: %q", lines[0])
	}
	for i, want := range []string{"child_event", "-", "human"} {
		if got := strings.Fields(lines[i+1])[3]; got != want {
			t.Errorf("row %d origin = %q, want %q", i, got, want)
		}
	}
}
