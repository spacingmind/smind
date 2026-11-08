package main

import (
	"reflect"
	"strings"
	"testing"
)

func colorQuestion() []map[string]any {
	return []map[string]any{
		questionEvent("req-1",
			map[string]any{"id": "q0", "text": "Color?", "kind": "single", "allowOther": true,
				"options": []map[string]any{{"id": "o0", "label": "Red"}, {"id": "o1", "label": "Blue"}}},
			map[string]any{"id": "q1", "text": "Sizes?", "kind": "multi",
				"options": []map[string]any{{"id": "s", "label": "S"}, {"id": "m", "label": "M"}, {"id": "l", "label": "L"}}},
		),
	}
}

func runAnswer(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	errOut = captureStderr(t, func() {
		out = captureStdout(t, func() { code = run(append([]string{"task", "answer"}, args...)) })
	})
	return
}

func wantAnswers(t *testing.T, calls []map[string]any, want []map[string]any) {
	t.Helper()
	if len(calls) != 1 {
		t.Fatalf("answerQuestion calls = %d, want 1", len(calls))
	}
	got := calls[0]
	if got["action"] != "answer" || got["runId"] != "run-1" || got["requestId"] != "req-1" {
		t.Fatalf("params = %v", got)
	}
	var norm []map[string]any
	for _, a := range got["answers"].([]any) {
		norm = append(norm, a.(map[string]any))
	}
	if !reflect.DeepEqual(norm, want) {
		t.Fatalf("answers = %v, want %v", norm, want)
	}
}

func TestTaskAnswer_SingleByNumber(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, out, errOut := runAnswer(t, "run-1", "--answer", "q0=2", "--answer", "q1=2")
	if code != 0 || !strings.Contains(out, "answered req-1") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	wantAnswers(t, answers(), []map[string]any{
		{"questionId": "q0", "optionIds": []any{"o1"}},
		{"questionId": "q1", "optionIds": []any{"m"}},
	})
}

func TestTaskAnswer_SingleByLabel(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, _, errOut := runAnswer(t, "run-1", "req-1", "--answer", "q0=blue", "--answer=q1=m")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	wantAnswers(t, answers(), []map[string]any{
		{"questionId": "q0", "optionIds": []any{"o1"}},
		{"questionId": "q1", "optionIds": []any{"m"}},
	})
}

func TestTaskAnswer_MultiByNumbers(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, _, errOut := runAnswer(t, "run-1", "--answer", "q0=1", "--answer", "q1=1,3")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	wantAnswers(t, answers(), []map[string]any{
		{"questionId": "q0", "optionIds": []any{"o0"}},
		{"questionId": "q1", "optionIds": []any{"s", "l"}},
	})
}

func TestTaskAnswer_OtherFreeText(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, _, errOut := runAnswer(t, "run-1", "--answer", "q0=Green please", "--answer", "q1=S")
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	wantAnswers(t, answers(), []map[string]any{
		{"questionId": "q0", "optionIds": []any{}, "text": "Green please"},
		{"questionId": "q1", "optionIds": []any{"s"}},
	})
}

func TestTaskAnswer_MissingQuestionErrorsBeforeCall(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, _, errOut := runAnswer(t, "run-1", "--answer", "q0=1")
	if code != 1 || !strings.Contains(errOut, "q1") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if n := len(answers()); n != 0 {
		t.Fatalf("daemon called %d times", n)
	}
}

func TestTaskAnswer_UnknownOptionErrors(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, _, errOut := runAnswer(t, "run-1", "--answer", "q0=1", "--answer", "q1=purple")
	if code != 1 || !strings.Contains(errOut, `question q1: no option matches "purple"`) {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if n := len(answers()); n != 0 {
		t.Fatalf("daemon called %d times", n)
	}
}

func TestTaskAnswer_DeclineWithNote(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	code, out, errOut := runAnswer(t, "run-1", "--decline", "--note", "not now")
	if code != 0 || !strings.Contains(out, "declined req-1") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	calls := answers()
	if len(calls) != 1 || calls[0]["action"] != "decline" || calls[0]["note"] != "not now" {
		t.Fatalf("calls = %v", calls)
	}
	if code, _, _ := runAnswer(t, "run-1", "--decline", "--note", strings.Repeat("x", 501)); code != 1 {
		t.Fatalf("long note exit = %d", code)
	}
}

func TestTaskAnswer_DefaultsToOldestPending(t *testing.T) {
	events := append(colorQuestion(),
		questionEvent("req-2", map[string]any{"id": "q0", "text": "Later?", "kind": "text"}))
	answers := fakeQuestionDaemon(t, "running", events, "")
	code, out, errOut := runAnswer(t, "run-1", "--decline")
	if code != 0 || !strings.Contains(out, "declined req-1") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if c := answers(); len(c) != 1 || c[0]["requestId"] != "req-1" {
		t.Fatalf("calls = %v", c)
	}
}

func TestTaskAnswer_NoFlagsNonTTYIsUsageNotBlock(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	old := answerStdinIsTerminal
	answerStdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { answerStdinIsTerminal = old })
	code, _, errOut := runAnswer(t, "run-1")
	if code != 2 || !strings.Contains(errOut, "usage: smind task answer") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if n := len(answers()); n != 0 {
		t.Fatalf("daemon called %d times", n)
	}
}

func TestTaskAnswer_InteractiveReadsAnswers(t *testing.T) {
	answers := fakeQuestionDaemon(t, "running", colorQuestion(), "")
	oldTTY, oldIn := answerStdinIsTerminal, answerInput
	answerStdinIsTerminal = func() bool { return true }
	answerInput = strings.NewReader("\nRed\n2,3\n")
	t.Cleanup(func() { answerStdinIsTerminal, answerInput = oldTTY, oldIn })
	code, out, errOut := runAnswer(t, "run-1")
	if code != 0 || !strings.Contains(out, "answer for q0> ") || !strings.Contains(out, "answered req-1") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	wantAnswers(t, answers(), []map[string]any{
		{"questionId": "q0", "optionIds": []any{"o0"}},
		{"questionId": "q1", "optionIds": []any{"m", "l"}},
	})
}

func TestTaskAnswer_DaemonErrorExit1(t *testing.T) {
	fakeQuestionDaemon(t, "running", colorQuestion(), "request already resolved")
	code, _, errOut := runAnswer(t, "run-1", "--decline")
	if code != 1 || !strings.Contains(errOut, "task answer: ") || !strings.Contains(errOut, "request already resolved") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestTaskAnswer_RefusedInsideAnAgentRun(t *testing.T) {
	t.Setenv("SMIND_RUN_ID", "run-1")
	if code := cmdTaskAnswer([]string{"run-1", "--decline"}); code != 1 {
		t.Fatalf("exit code = %d, want 1 (refused inside an agent run)", code)
	}
}
