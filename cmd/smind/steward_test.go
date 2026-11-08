package main

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

type fakeStewardCaller struct {
	method string
	params map[string]any
	result any
	dialed bool
}

func (f *fakeStewardCaller) Call(_ context.Context, method string, params, out any) error {
	f.method = method
	f.params, _ = params.(map[string]any)
	raw, _ := json.Marshal(f.result)
	return json.Unmarshal(raw, out)
}
func (f *fakeStewardCaller) Close() error { return nil }

func runStewardCLI(t *testing.T, f *fakeStewardCaller, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	f.dialed = false
	code := runSteward(args, stewardIO{out: &o, errOut: &e,
		dial: func(context.Context) (daemonCaller, error) { f.dialed = true; return f, nil }})
	return code, o.String(), e.String()
}

const stewardSha = "0123456789abcdef0123456789abcdef01234567"

func TestCLI_StewardReviewSendsParams(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"id": 11, "taskId": 7, "status": "pending", "headSha": stewardSha, "stale": "", "mergeReady": false}}
	code, out, errOut := runStewardCLI(t, f, "review", "7", "--sha", stewardSha)
	if code != 0 || !strings.Contains(out, "#11 task 7 pending") || !strings.Contains(out, "head 0123456") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "steward.review" || f.params["taskId"] != int64(7) || f.params["commitSha"] != stewardSha {
		t.Fatalf("call = %s %v", f.method, f.params)
	}

	f = &fakeStewardCaller{result: map[string]any{"id": 12, "taskId": 8, "verdict": "CHANGES", "status": "finished", "headSha": strings.Repeat("b", 40), "stale": "head moved", "mergeReady": false}}
	code, out, errOut = runStewardCLI(t, f, "review", "8")
	if code != 0 || !strings.Contains(out, "#12 task 8 CHANGES") || !strings.Contains(out, "stale (head moved)") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "steward.review" || f.params["taskId"] != int64(8) {
		t.Fatalf("call = %s %v", f.method, f.params)
	}
	if _, has := f.params["commitSha"]; has {
		t.Fatalf("commitSha sent without --sha: %v", f.params)
	}
}

func TestCLI_StewardListShowCancelCallMethods(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"items": []any{
		map[string]any{"id": 3, "taskId": 7, "verdict": "ACCEPTED", "headSha": stewardSha, "stale": "", "mergeReady": true},
		map[string]any{"id": 2, "taskId": 7, "verdict": "CHANGES", "headSha": strings.Repeat("c", 40), "stale": "head moved", "mergeReady": false},
	}}}
	code, out, errOut := runStewardCLI(t, f, "list", "7", "--limit", "3")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if !strings.Contains(out, "#3 task 7 ACCEPTED") || !strings.Contains(out, "#2 task 7 CHANGES") ||
		!strings.Contains(out, "mergeReady=true") || !strings.Contains(out, "stale") {
		t.Fatalf("list output = %q", out)
	}
	if f.method != "steward.list" || f.params["taskId"] != int64(7) || f.params["limit"] != int64(3) {
		t.Fatalf("call = %s %v", f.method, f.params)
	}

	f = &fakeStewardCaller{result: map[string]any{"items": []any{}}}
	code, out, _ = runStewardCLI(t, f, "list")
	if code != 0 || !strings.Contains(out, "no reviews") || f.params["taskId"] != nil || f.params["limit"] != nil {
		t.Fatalf("exit %d out %q params %v", code, out, f.params)
	}

	f = &fakeStewardCaller{result: map[string]any{"id": 5, "taskId": 7, "verdict": "CHANGES", "headSha": stewardSha, "stale": "", "mergeReady": false,
		"summary": "two findings", "gates": []any{map[string]any{"id": "ci", "state": "pass"}, map[string]any{"id": "scope", "state": "fail"}}}}
	code, out, errOut = runStewardCLI(t, f, "show", "5")
	if code != 0 || !strings.Contains(out, "summary: two findings") || !strings.Contains(out, "ci:pass") || !strings.Contains(out, "scope:fail") {
		t.Fatalf("show exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "steward.get" || f.params["reviewId"] != int64(5) {
		t.Fatalf("call = %s %v", f.method, f.params)
	}

	f = &fakeStewardCaller{result: map[string]any{"id": 5, "taskId": 7, "status": "running", "headSha": stewardSha, "stale": "", "mergeReady": false}}
	code, out, errOut = runStewardCLI(t, f, "cancel", "5")
	if code != 0 || !strings.Contains(out, "#5 task 7 running") {
		t.Fatalf("cancel exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "steward.cancel" || f.params["reviewId"] != int64(5) {
		t.Fatalf("call = %s %v", f.method, f.params)
	}
}

func TestCLI_StewardLineShowsStaleReason(t *testing.T) {
	// The daemon's View sends `stale` as a string reason ("" = fresh); the
	// line shows the reason, and a missing or empty field reads as fresh.
	for _, tc := range []struct {
		name  string
		stale any
		want  string
	}{
		{"reason", "head moved", "stale (head moved)"},
		{"empty reason", "", "fresh"},
		{"missing field", nil, "fresh"},
	} {
		m := map[string]any{"id": 3, "taskId": 7, "verdict": "ACCEPTED", "headSha": stewardSha, "mergeReady": true}
		if tc.stale != nil {
			m["stale"] = tc.stale
		}
		if got := stewardLine(m); !strings.Contains(got, " "+tc.want+" ") {
			t.Fatalf("%s: line = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// steward.review's reply names the fields reviewId/commitSha, not a View's
// id/headSha; the line reads both spellings.
func TestCLI_StewardLineReadsStartResult(t *testing.T) {
	m := map[string]any{"reviewId": 4, "taskId": 2, "commitSha": stewardSha, "status": "running"}
	want := "#4 task 2 running head " + stewardSha[:7] + " fresh mergeReady=-"
	if got := stewardLine(m); got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
	m = map[string]any{"reviewId": 5, "taskId": 2, "status": "pending"}
	if got := stewardLine(m); !strings.Contains(got, "head - ") {
		t.Fatalf("line = %q, want - for both heads missing", got)
	}
}

func TestCLI_StewardRejectsBadIds(t *testing.T) {
	f := &fakeStewardCaller{}
	for _, args := range [][]string{
		{"review", "0"}, {"review", "x"}, {"show", "0"}, {"show", "-3"}, {"cancel", "nope"}, {"list", "0"},
		{"review", "7", "--sha", "abc"}, {"review", "7", "--sha", stewardSha[:39]}, {"list", "7", "--limit", "0"},
	} {
		code, _, errOut := runStewardCLI(t, f, args...)
		if code != 2 || f.dialed {
			t.Fatalf("%v exit %d dialed %v err %q", args, code, f.dialed, errOut)
		}
	}
}

// The steward surface has no verdict-acting verb: a review is advice, the
// Human reads it and merges. The words may appear nowhere in the steward
// usage or in the main usage's steward lines, except inside the closing
// sentence (which is removed before checking).
func TestCLI_StewardHasNoApproveOrMerge(t *testing.T) {
	f := &fakeStewardCaller{}
	for _, sub := range []string{"approve", "merge", "accept", "resolve", "send"} {
		code, out, errOut := runStewardCLI(t, f, sub, "5")
		if code != 2 || f.dialed || !strings.Contains(errOut, "usage: smind steward") {
			t.Fatalf("steward %s: exit %d dialed %v err %q out %q", sub, code, f.dialed, errOut, out)
		}
	}
	var mainUsage bytes.Buffer
	printUsage(&mainUsage)
	var stewardLines []string
	for _, l := range strings.Split(mainUsage.String(), "\n") {
		if strings.Contains(l, "smind steward") {
			stewardLines = append(stewardLines, l)
		}
	}
	if len(stewardLines) == 0 {
		t.Fatalf("main usage has no steward line")
	}
	sentence := "smind never merges; you read it and you merge."
	for name, text := range map[string]string{
		"steward usage":   cmdStewardUsage,
		"main usage line": strings.Join(stewardLines, "\n"),
	} {
		lower := strings.ToLower(strings.ReplaceAll(text, sentence, ""))
		if bad := regexp.MustCompile(`\b(approve|merge)\b`).FindString(lower); bad != "" {
			t.Errorf("%s contains %q outside the closing sentence", name, bad)
		}
	}
}

func TestCLI_StewardJSONFlagPassesThrough(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"id": 9, "taskId": 7, "verdict": "ACCEPTED", "headSha": stewardSha, "stale": "", "mergeReady": true,
		"gates": []any{map[string]any{"id": "ci", "state": "pass"}}}}
	code, out, errOut := runStewardCLI(t, f, "show", "9", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &got); err != nil || got["verdict"] != "ACCEPTED" {
		t.Fatalf("--json output = %q (%v)", out, err)
	}

	f = &fakeStewardCaller{result: map[string]any{"items": []any{map[string]any{"id": 1, "taskId": 7}}}}
	code, out, _ = runStewardCLI(t, f, "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var arr []any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &arr); err != nil || len(arr) != 1 {
		t.Fatalf("--json list output = %q (%v)", out, err)
	}
}

// A two-axis review's show output: the axes block after summary and gates,
// the spec criteria under the spec pass, the findings tagged by axis. Cost
// never appears (it lives in the Usage overview), so a `costUsd` anywhere
// in the output is a bug.
func TestStewardShow_PrintsAxesBlock(t *testing.T) {
	longEvidence := strings.Repeat("e", 118) + "\nabc"
	f := &fakeStewardCaller{result: map[string]any{
		"id": 21, "taskId": 7, "verdict": "CHANGES", "headSha": stewardSha, "stale": "", "mergeReady": false,
		"summary": "two axes",
		"gates":   []any{map[string]any{"id": "ci", "state": "pass"}},
		"axes": []any{
			map[string]any{"axis": "standards", "status": "done", "seatVerdict": "CHANGES", "summary": "one finding", "model": "claude-haiku-5-5", "costUsd": 0.42, "runId": "run-1"},
			map[string]any{"axis": "spec", "status": "done", "seatVerdict": "ACCEPTED", "summary": "spec met", "model": "claude-sonnet-5-5", "costUsd": 0.1,
				"criteria": []any{
					map[string]any{"id": "crit-1", "status": "met", "evidence": "plan line 4 says so"},
					map[string]any{"id": "crit-2", "status": "unmet", "evidence": longEvidence},
				}},
		},
		"findings": []any{
			map[string]any{"axis": "standards", "file": "a.go", "line": 10, "severity": "major", "why": "unused var", "check": "go-vet", "rule": "std-1", "costUsd": 0.42},
			map[string]any{"axis": "spec", "file": "b.go", "line": 3, "severity": "minor", "why": "missing case", "check": "spec"},
			map[string]any{"file": "c.go", "line": 5, "severity": "info", "why": "nit", "check": "misc"},
		},
	}}
	code, out, errOut := runStewardCLI(t, f, "show", "21")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	want := "#21 task 7 CHANGES head 0123456 fresh mergeReady=false\n" +
		"  summary: two axes\n" +
		"  gate ci:pass\n" +
		"  axes:\n" +
		"    standards: changes model claude-haiku-5-5\n" +
		"    spec: accepted model claude-sonnet-5-5\n" +
		"      crit-1 met: plan line 4 says so\n" +
		"      crit-2 unmet: " + strings.Repeat("e", 118) + " a\n" +
		"  findings:\n" +
		"    [standards] a.go:10 [major] unused var (check: go-vet) rule: std-1\n" +
		"    [spec] b.go:3 [minor] missing case (check: spec)\n" +
		"    [-] c.go:5 [info] nit (check: misc)\n"
	if out != want {
		t.Fatalf("show output =\n%q\nwant\n%q", out, want)
	}
	for _, bad := range []string{"costUsd", "$", "0.42"} {
		if strings.Contains(out, bad) {
			t.Fatalf("show output leaks cost (%q): %q", bad, out)
		}
	}
}

func TestStewardShow_SkippedAndBlockedAxes(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{
		"id": 22, "taskId": 7, "status": "blocked", "headSha": stewardSha, "stale": "", "mergeReady": false,
		"axes": []any{
			map[string]any{"axis": "standards", "status": "blocked", "blockedReason": "timeout"},
			map[string]any{"axis": "spec", "status": "skipped", "blockedReason": "no_spec"},
		},
	}}
	code, out, errOut := runStewardCLI(t, f, "show", "22")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if !strings.Contains(out, "    standards: did not complete (timeout)\n") {
		t.Fatalf("blocked axis line missing: %q", out)
	}
	if !strings.Contains(out, "    spec: skipped (no spec available)\n") {
		t.Fatalf("skipped axis line missing: %q", out)
	}
}

// A single-axis review's show output must stay exactly as it was: no axes
// block, no findings block, even when the result carries findings.
func TestStewardShow_WithoutAxesIsUnchanged(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"id": 5, "taskId": 7, "verdict": "CHANGES", "headSha": stewardSha, "stale": "", "mergeReady": false,
		"summary": "two findings", "gates": []any{map[string]any{"id": "ci", "state": "pass"}, map[string]any{"id": "scope", "state": "fail"}},
		"findings": []any{map[string]any{"file": "a.go", "line": 10, "severity": "major", "why": "unused var", "check": "go-vet"}}}}
	code, out, errOut := runStewardCLI(t, f, "show", "5")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	want := "#5 task 7 CHANGES head 0123456 fresh mergeReady=false\n" +
		"  summary: two findings\n" +
		"  gate ci:pass\n" +
		"  gate scope:fail\n"
	if out != want {
		t.Fatalf("show output =\n%q\nwant\n%q", out, want)
	}
}

func TestStewardShow_MalformedAxesNeverPanic(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{
		"id": 23, "taskId": 7, "status": "running", "headSha": stewardSha, "stale": "", "mergeReady": false,
		"axes": []any{
			"standards", 42, nil,
			map[string]any{"axis": "spec", "criteria": "nope"},
			map[string]any{"status": "done"},
			map[string]any{"axis": 7, "status": "pending", "criteria": []any{"crit"}},
		},
		"findings": []any{"oops", 3, nil, map[string]any{"axis": "spec"}},
	}}
	code, out, errOut := runStewardCLI(t, f, "show", "23")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if !strings.Contains(out, "  axes:\n") || !strings.Contains(out, "  findings:\n") ||
		!strings.Contains(out, "    [spec] -:- [-] - (check: -)\n") {
		t.Fatalf("show output = %q", out)
	}
}

func TestStewardReview_AxisFlagSendsAxes(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"reviewId": 31, "taskId": 7, "commitSha": stewardSha, "status": "running"}}
	axesParam := func(want ...string) {
		t.Helper()
		got, ok := f.params["axes"].([]string)
		if !ok || len(got) != len(want) {
			t.Fatalf("axes = %#v want %v (params %v)", f.params["axes"], want, f.params)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("axes = %v want %v", got, want)
			}
		}
	}
	code, out, errOut := runStewardCLI(t, f, "review", "7", "--axis", "spec")
	if code != 0 || !strings.Contains(out, "#31 task 7 running") || errOut != "" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	axesParam("spec")

	code, _, _ = runStewardCLI(t, f, "review", "7", "--axis", "spec", "--axis", "standards")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	axesParam("spec", "standards")

	code, _, _ = runStewardCLI(t, f, "review", "7", "--axis", "standards", "--axis", "spec", "--axis", "standards")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	axesParam("standards", "spec")

	code, _, _ = runStewardCLI(t, f, "review", "7")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, has := f.params["axes"]; has {
		t.Fatalf("axes sent without --axis: %v", f.params)
	}
}

func TestStewardReview_AxisFlagValidation(t *testing.T) {
	f := &fakeStewardCaller{result: map[string]any{"id": 1, "taskId": 7}}
	code, _, errOut := runStewardCLI(t, f, "review", "7", "--axis")
	if code != 2 || f.dialed || !strings.Contains(errOut, "steward: --axis needs standards or spec") {
		t.Fatalf("missing value: exit %d dialed %v err %q", code, f.dialed, errOut)
	}
	code, _, errOut = runStewardCLI(t, f, "review", "7", "--axis", "other")
	if code != 2 || f.dialed || !strings.Contains(errOut, `steward: invalid axis "other" (want standards or spec)`) {
		t.Fatalf("bad value: exit %d dialed %v err %q", code, f.dialed, errOut)
	}
	for _, args := range [][]string{
		{"show", "5", "--axis", "spec"}, {"list", "--axis", "standards"}, {"cancel", "5", "--axis", "spec"},
	} {
		code, _, errOut = runStewardCLI(t, f, args...)
		if code != 2 || f.dialed || !strings.Contains(errOut, "usage: smind steward") {
			t.Fatalf("%v: exit %d dialed %v err %q", args, code, f.dialed, errOut)
		}
	}
}

func TestStewardUsage_HasNoApproveOrMerge(t *testing.T) {
	if !strings.Contains(cmdStewardUsage, "[--axis standards|spec]") {
		t.Fatalf("usage lacks --axis:\n%s", cmdStewardUsage)
	}
	lower := strings.ToLower(cmdStewardUsage)
	if strings.Contains(lower, "approve") || strings.Contains(lower, "merge ") {
		t.Fatalf("usage shows a verdict-acting command:\n%s", cmdStewardUsage)
	}
	if !strings.Contains(cmdStewardUsage, "A review is advice: smind never merges; you read it and you merge.") {
		t.Fatalf("usage lost its closing rule line:\n%s", cmdStewardUsage)
	}
}

// TestStewardShow_TerminalEscapesAndForgedLinesAreNeutralised: the seat's and
// the PR author's text reaches `show`; an escape sequence or a line break in it
// must not move the cursor, recolour the terminal or forge another output line.
func TestStewardShow_TerminalEscapesAndForgedLinesAreNeutralised(t *testing.T) {
	res := map[string]any{
		"id": float64(7), "taskId": float64(3), "verdict": "CHANGES", "headSha": strings.Repeat("a", 40), "mergeReady": false,
		"summary": "ok\x1b[2J\nsummary line two",
		"axes": []any{
			map[string]any{"axis": "spec", "status": "done", "seatVerdict": "CHANGES", "model": "m\x1b[31m", "criteria": []any{
				map[string]any{"id": "AC1", "status": "met", "evidence": "fine\x1b]0;pwned\x07\n  gate ci:pass"},
			}},
		},
		"findings": []any{map[string]any{"axis": "spec", "file": "a.go\x1b[H", "line": float64(1), "severity": "P1", "why": "bad\n  gate scope:pass", "check": "go test\r", "rule": "r\x1b[0m"}},
	}
	code, got, errOut := runStewardCLI(t, &fakeStewardCaller{result: res}, "show", "7")
	if code != 0 {
		t.Fatalf("show = %d: %s", code, errOut)
	}
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) || strings.ContainsRune(got, '\r') {
		t.Fatalf("output carries a control character: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "gate ") {
			t.Fatalf("a value forged an output line: %q in %q", line, got)
		}
	}
}
