package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runAutomationCLI(t *testing.T, f *fakePluginDaemon, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = runAutomation(args, pluginIO{in: strings.NewReader(""), out: &o, errOut: &e,
		dial: func(context.Context) (daemonCaller, error) { return f, nil }})
	return code, o.String(), e.String()
}

func autoView(id int64, enabled bool) map[string]any {
	return map[string]any{"id": id, "name": "pilot", "enabled": enabled, "status": "active",
		"signal":   map[string]any{"kind": "github.workflow_run", "workflow": "Windows full suite", "branch": "develop"},
		"template": map[string]any{}, "rails": map[string]any{}}
}

func TestAutomationCLI_CreateFlags_StartsDisabled(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"automation.create": func(map[string]any) (any, error) { return autoView(3, false), nil },
	}}
	code, out, errOut := runAutomationCLI(t, f, "create", "--name", "pilot", "--workspace", "1", "--profile", "2", "--model", "claude-sonnet-5-5",
		"--workflow", "Windows full suite", "--title", "t", "--prompt", "p", "--budget", "3", "--memory")
	if code != 0 || !strings.Contains(out, "DISABLED") || !strings.Contains(out, "smind automation enable 3") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	p := f.calls[0].params
	sig := p["signal"].(map[string]any)
	tpl := p["template"].(map[string]any)
	if sig["kind"] != "github.workflow_run" || tpl["budgetUsd"] != float64(3) || tpl["mode"] != "default" || p["workspaceId"] != float64(1) {
		t.Fatalf("params = %+v", p)
	}
	if f.called("automation.enable") {
		t.Fatal("create armed the automation")
	}
}

func TestAutomationCLI_CreateFromFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.json")
	_ = os.WriteFile(file, []byte(`{"name":"x","workspaceId":1,"signal":{"kind":"cron","cron":"17 19 * * *"}}`), 0o600)
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"automation.create": func(map[string]any) (any, error) { return autoView(4, false), nil },
	}}
	if code, _, errOut := runAutomationCLI(t, f, "create", "--file", file); code != 0 {
		t.Fatalf("code %d %s", code, errOut)
	}
	if f.calls[0].params["name"] != "x" {
		t.Fatalf("file not sent: %+v", f.calls[0].params)
	}
}

func TestAutomationCLI_CreateNeedsExactlyOneSignal(t *testing.T) {
	f := &fakePluginDaemon{}
	if code, _, _ := runAutomationCLI(t, f, "create", "--name", "n"); code != 2 {
		t.Fatalf("code %d, want 2", code)
	}
	if len(f.calls) != 0 {
		t.Fatal("called the daemon")
	}
}

func TestAutomationCLI_EnableResumeFireAudit(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"automation.enable": func(map[string]any) (any, error) { return autoView(3, true), nil },
		"automation.resume": func(map[string]any) (any, error) { return autoView(3, true), nil },
		"automation.fire": func(map[string]any) (any, error) {
			return map[string]any{"id": 9, "decision": "fire", "taskId": 12}, nil
		},
		"automation.audit": func(p map[string]any) (any, error) {
			return map[string]any{"rows": []any{map[string]any{"id": 9, "decision": "capped", "reason": "rate: 2 runs", "createdAt": "2026-10-27T12:00:00Z"}}}, nil
		},
	}}
	if code, out, _ := runAutomationCLI(t, f, "enable", "3"); code != 0 || !strings.Contains(out, "enabled") {
		t.Fatalf("enable: %d %q", code, out)
	}
	if code, out, _ := runAutomationCLI(t, f, "resume", "3"); code != 0 || !strings.Contains(out, "resumed") {
		t.Fatalf("resume: %d %q", code, out)
	}
	if code, out, _ := runAutomationCLI(t, f, "fire", "3"); code != 0 || !strings.Contains(out, "task #12") {
		t.Fatalf("fire: %d %q", code, out)
	}
	code, out, _ := runAutomationCLI(t, f, "audit", "3", "--limit", "5")
	if code != 0 || !strings.Contains(out, "capped") || !strings.Contains(out, "rate: 2 runs") {
		t.Fatalf("audit: %d %q", code, out)
	}
	last := f.calls[len(f.calls)-1].params
	if last["limit"] != float64(5) || last["id"] != float64(3) {
		t.Fatalf("audit params = %+v", last)
	}
	rawCode, rawOut, _ := runAutomationCLI(t, f, "audit", "--json", "3")
	if rawCode != 0 || !strings.Contains(rawOut, `"decision": "capped"`) {
		t.Fatalf("audit --json: %d %q", rawCode, rawOut)
	}
}

func TestAutomationCLI_FireNotFiredExitsNonZero(t *testing.T) {
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"automation.fire": func(map[string]any) (any, error) {
			return map[string]any{"id": 9, "decision": "capped", "reason": "running: a task is still running"}, nil
		},
	}}
	code, out, _ := runAutomationCLI(t, f, "fire", "3")
	if code == 0 || !strings.Contains(out, "capped") {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestAutomationCLI_ListOffBanner_ShowPausedReason(t *testing.T) {
	paused := autoView(3, true)
	paused["status"], paused["pausedReason"] = "paused", "paused after 3 consecutive failed runs; last: task #4 ended `error`"
	f := &fakePluginDaemon{handlers: map[string]func(map[string]any) (any, error){
		"automation.list": func(map[string]any) (any, error) {
			return map[string]any{"enabled": false, "paused": false, "automations": []any{paused}}, nil
		},
		"automation.get": func(map[string]any) (any, error) { return paused, nil },
	}}
	_, out, _ := runAutomationCLI(t, f, "list")
	if !strings.Contains(out, "Automations are OFF") || !strings.Contains(out, "paused") {
		t.Fatalf("list = %q", out)
	}
	_, out, _ = runAutomationCLI(t, f, "show", "3")
	if !strings.Contains(out, "PAUSED: paused after 3 consecutive") || !strings.Contains(out, "smind automation resume 3") {
		t.Fatalf("show = %q", out)
	}
}

func TestAutomationCLI_BadUsage(t *testing.T) {
	f := &fakePluginDaemon{}
	for _, args := range [][]string{{}, {"nope"}, {"enable"}, {"enable", "x"}, {"audit"}, {"memory", "0"}} {
		if code, _, _ := runAutomationCLI(t, f, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

// TestMCPTools_NoAutomationTool: an agent can never create, enable or fire an
// automation (ADR-0044 section 5), so the MCP catalogue has no such tool.
func TestMCPTools_NoAutomationTool(t *testing.T) {
	env := newMCPSession(t)
	for _, name := range mcpToolNames(t, env.cs) {
		if strings.Contains(strings.ToLower(name), "automation") {
			t.Fatalf("tools/list contains %q", name)
		}
	}
}
