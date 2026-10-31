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
