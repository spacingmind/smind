package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeValCaller struct {
	method string
	params map[string]any
	result any
}

func (f *fakeValCaller) Call(_ context.Context, method string, params, out any) error {
	f.method = method
	f.params, _ = params.(map[string]any)
	raw, _ := json.Marshal(f.result)
	return json.Unmarshal(raw, out)
}
func (f *fakeValCaller) Close() error { return nil }

func runValCLI(t *testing.T, f *fakeValCaller, files map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	code := runValidation(args, validationIO{out: &o, errOut: &e,
		dial: func(context.Context) (daemonCaller, error) { return f, nil },
		readFile: func(p string) ([]byte, error) {
			if s, ok := files[p]; ok {
				return []byte(s), nil
			}
			return nil, context.Canceled
		}})
	return code, o.String(), e.String()
}

func TestValidationCLI_SubmitSendsFileInline(t *testing.T) {
	f := &fakeValCaller{result: map[string]any{"id": 3, "taskId": 7, "version": 1, "sha256": "abc"}}
	code, out, errOut := runValCLI(t, f, map[string]string{"c.md": "---\nv: 1\n---\n"}, "contract", "submit", "7", "c.md")
	if code != 0 || !strings.Contains(out, "version 1") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if f.method != "validation.contract.submit" || f.params["source"] != "inline" || f.params["body"] != "---\nv: 1\n---\n" {
		t.Fatalf("call = %s %v", f.method, f.params)
	}
}

func TestValidationCLI_ShowPrintsMergeReadyLabel(t *testing.T) {
	f := &fakeValCaller{result: map[string]any{"id": 4, "taskId": 7, "status": "pass", "commitSha": strings.Repeat("a", 40), "contractVersion": 2, "mergeReady": false, "notReadyReason": "stale: the branch head moved after this verdict"}}
	code, out, _ := runValCLI(t, f, nil, "show", "4")
	if code != 0 || !strings.Contains(out, "not merge-ready (stale") || strings.Contains(out, "merge-ready ·") {
		t.Fatalf("exit %d out %q", code, out)
	}
}

func TestValidationCLI_NoMergeOrVerdictCommand(t *testing.T) {
	f := &fakeValCaller{}
	for _, args := range [][]string{{"merge", "1"}, {"verdict", "submit", "1"}, {"waive", "1"}, {}} {
		if code, _, _ := runValCLI(t, f, nil, args...); code != 2 {
			t.Fatalf("%v exit %d, want usage (2)", args, code)
		}
	}
}
