package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func stubPermissionDaemon(t *testing.T, reply any, err error) *[]map[string]any {
	t.Helper()
	var calls []map[string]any
	old := policyDial
	policyDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, result any) error {
			p, _ := params.(map[string]any)
			p["method"] = method
			calls = append(calls, p)
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(reply)
			return json.Unmarshal(raw, result)
		}, func() {}, nil
	}
	t.Cleanup(func() { policyDial = old })
	return &calls
}

func TestPermissionShowReveal_Pending(t *testing.T) {
	calls := stubPermissionDaemon(t, map[string]any{"index": 1, "bytes": 9, "text": "line\x1b[31m red\n", "redacted": true}, nil)
	var out, errb bytes.Buffer
	if code := runPermission(context.Background(), []string{"show", "--reveal", "--index", "1", "req-1"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	c := (*calls)[0]
	if c["method"] != "permission.revealHeredoc" || c["permissionId"] != "req-1" || c["index"] != 1 {
		t.Errorf("call = %v", c)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "line") || !strings.Contains(errb.String(), "redacted") {
		t.Errorf("stdout %q stderr %q", out.String(), errb.String())
	}
}

func TestPermissionShowReveal_ToolCallAndErrors(t *testing.T) {
	calls := stubPermissionDaemon(t, map[string]any{"text": "body\n"}, nil)
	var out, errb bytes.Buffer
	if code := runPermission(context.Background(), []string{"show", "--reveal", "--run", "r1", "--call", "c1"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	c := (*calls)[0]
	if c["method"] != "run.revealToolHeredoc" || c["runId"] != "r1" || c["toolCallId"] != "c1" || c["index"] != 0 {
		t.Errorf("call = %v", c)
	}
	for _, args := range [][]string{{}, {"show", "req-1"}, {"show", "--reveal"}, {"show", "--reveal", "--run", "r1", "x"}, {"show", "--reveal", "--index", "-1", "req"}} {
		if code := runPermission(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}
