package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCLI_PermissionGrantsList(t *testing.T) {
	calls := stubPermissionDaemon(t, map[string]any{"grants": []map[string]any{{"id": 4, "workspaceId": 2, "status": "active", "class": "runner", "runnerOptIn": true, "command": "make build\x1b[31m", "folder": "/repo", "expiresAt": "2026-12-15T00:00:00Z", "useCount": 3}}}, nil)
	var out, errb bytes.Buffer
	if code := runPermission(context.Background(), []string{"grants", "ls", "--project", "2"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	c := (*calls)[0]
	if c["method"] != "permission.grants.list" || c["workspaceId"] != int64(2) {
		t.Errorf("call = %v", c)
	}
	if strings.Contains(out.String(), "\x1b") || !strings.Contains(out.String(), "make build") || !strings.Contains(out.String(), "active") || !strings.Contains(out.String(), "runner (opted in)") {
		t.Errorf("stdout %q", out.String())
	}
}

func TestCLI_PermissionGrantsRevokeAndErrors(t *testing.T) {
	calls := stubPermissionDaemon(t, map[string]any{"revoked": true}, nil)
	var out, errb bytes.Buffer
	if code := runPermission(context.Background(), []string{"grants", "revoke", "4"}, &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if c := (*calls)[0]; c["method"] != "permission.grants.revoke" || c["grantId"] != int64(4) {
		t.Errorf("call = %v", c)
	}
	if !strings.Contains(out.String(), "revoked") {
		t.Errorf("stdout %q", out.String())
	}
	for _, args := range [][]string{{"grants"}, {"grants", "bogus"}, {"grants", "revoke"}, {"grants", "revoke", "x"}, {"grants", "revoke", "1", "2"}, {"grants", "ls", "--project", "x"}, {"grants", "ls", "extra"}} {
		if code := runPermission(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}
