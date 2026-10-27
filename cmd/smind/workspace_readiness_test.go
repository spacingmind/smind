package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/readiness"
	"github.com/spacingmind/smind/internal/store"
)

type fakeReadinessDaemon struct{}

func (fakeReadinessDaemon) Call(_ context.Context, method string, params, out any) error {
	var v any
	switch method {
	case "workspace.list":
		v = []store.Workspace{{ID: 1, Title: "alpha"}, {ID: 2, Title: "beta"}}
	case "workspace.get":
		id := params.(map[string]any)["id"].(int64)
		if id != 1 {
			return errors.New("workspace not found")
		}
		v = store.Workspace{ID: 1, Title: "alpha"}
	case "workspace.readiness":
		v = map[string]any{"findings": []readiness.Finding{
			{ID: "docs", Status: "pass", Title: "README or docs folder", Evidence: "README.md"},
			{ID: "ci-config", Status: "warn", Title: "CI configuration is present", Evidence: "no CI config found", Fix: "Add a CI workflow."},
		}}
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}

func TestWorkspaceReadiness_CLI_Table_And_JSON(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runWorkspaceReadiness(context.Background(), fakeReadinessDaemon{}, 0, false, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"workspace 1: alpha", "workspace 2: beta", "pass", "warn", "CI configuration is present", "fix:", "Add a CI workflow."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Count(out.String(), "fix:") != 2 {
		t.Errorf("want a fix line only for warns:\n%s", out.String())
	}

	out.Reset()
	if code := runWorkspaceReadiness(context.Background(), fakeReadinessDaemon{}, 1, true, &out, &errb); code != 0 {
		t.Fatalf("json exit %d", code)
	}
	var reps []readinessReport
	if err := json.Unmarshal(out.Bytes(), &reps); err != nil || len(reps) != 1 || reps[0].WorkspaceID != 1 || len(reps[0].Findings) != 2 || reps[0].Findings[1].Status != "warn" {
		t.Fatalf("json = %s (%v)", out.String(), err)
	}

	errb.Reset()
	if code := runWorkspaceReadiness(context.Background(), fakeReadinessDaemon{}, 9, false, &out, &errb); code != 1 || !strings.Contains(errb.String(), "not found") {
		t.Fatalf("unknown workspace: exit %d, %q", code, errb.String())
	}
}
