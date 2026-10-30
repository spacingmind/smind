package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// cooldownCall is one RPC the stubbed daemon connection saw.
type cooldownCall struct {
	method string
	params any
}

// stubCooldown points cooldownDial at a fake caller that returns result (or
// callErr) and records each call's method and params.
func stubCooldown(t *testing.T, result any, callErr error) *[]cooldownCall {
	t.Helper()
	var seen []cooldownCall
	old := cooldownDial
	cooldownDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, method string, params any, out any) error {
			seen = append(seen, cooldownCall{method, params})
			if callErr != nil {
				return callErr
			}
			raw, _ := json.Marshal(result)
			return json.Unmarshal(raw, out)
		}, func() {}, nil
	}
	t.Cleanup(func() { cooldownDial = old })
	return &seen
}

func runCooldownCmd(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = runCooldown(context.Background(), args, &o, &e)
	return code, o.String(), e.String()
}

func TestCooldownClearCallsRPCAndReportsWoken(t *testing.T) {
	seen := stubCooldown(t, map[string]any{"provider": "glm", "cleared": true, "woken": 2}, nil)
	code, out, errOut := runCooldownCmd("clear", "glm")
	if code != 0 || errOut != "" || !strings.Contains(out, "retrying 2 held run(s)") {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	if len(*seen) != 1 {
		t.Fatalf("calls = %#v", *seen)
	}
	if got := (*seen)[0].method; got != "provider.clearCooldown" {
		t.Errorf("method = %q", got)
	}
	if m, ok := (*seen)[0].params.(map[string]any); !ok || m["provider"] != "glm" || len(m) != 1 {
		t.Errorf("params = %#v", (*seen)[0].params)
	}
}

func TestCooldownClearNoCooldown(t *testing.T) {
	stubCooldown(t, map[string]any{"provider": "glm", "cleared": false, "woken": 0}, nil)
	code, out, errOut := runCooldownCmd("clear", "glm")
	if code != 0 || errOut != "" || !strings.Contains(out, "glm had no active cooldown.") {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	stubCooldown(t, map[string]any{"provider": "glm", "cleared": true, "woken": 0}, nil)
	code, out, _ = runCooldownCmd("clear", "glm")
	if code != 0 || !strings.Contains(out, "Cleared the cooldown on glm.") || strings.Contains(out, "retrying") {
		t.Fatalf("cleared with nothing held: code %d stdout %q", code, out)
	}
}

func TestCooldownClearRequiresProvider(t *testing.T) {
	seen := stubCooldown(t, nil, nil)
	for _, args := range [][]string{{"clear"}, {"clear", ""}, {"clear", "glm", "extra"}} {
		code, out, errOut := runCooldownCmd(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "smind cooldown clear <provider>") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, out, errOut)
		}
	}
	if len(*seen) != 0 {
		t.Errorf("daemon was dialed: %#v", *seen)
	}
}

func TestCooldownLsListsOnlyCoolingProviders(t *testing.T) {
	until := time.Now().Add(80 * time.Minute).UTC().Format(time.RFC3339)
	stubCooldown(t, map[string]any{"providers": []map[string]any{
		{"id": "glm", "label": "GLM", "cooldownUntil": until},
		{"id": "openai", "label": "OpenAI"},
	}}, nil)
	code, out, errOut := runCooldownCmd("ls")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	for _, want := range []string{"PROVIDER", "RESETS AT", "IN", "glm", "1h"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "openai") {
		t.Fatalf("provider that is not cooling is listed:\n%s", out)
	}
}

func TestCooldownLsEmpty(t *testing.T) {
	stubCooldown(t, map[string]any{"providers": []map[string]any{
		{"id": "glm", "label": "GLM"},
	}}, nil)
	code, out, errOut := runCooldownCmd("ls")
	if code != 0 || errOut != "" || !strings.Contains(out, "No provider is cooling down.") {
		t.Fatalf("code %d stdout %q stderr %q", code, out, errOut)
	}
	if strings.Contains(out, "PROVIDER") {
		t.Fatalf("table printed although nothing is cooling:\n%s", out)
	}
}

func TestCooldownRPCErrorExitsOne(t *testing.T) {
	stubCooldown(t, nil, errors.New("boom"))
	code, out, errOut := runCooldownCmd("ls")
	if code != 1 || out != "" || strings.TrimSpace(errOut) != "smind: boom" {
		t.Fatalf("ls: code %d stdout %q stderr %q", code, out, errOut)
	}
	stubCooldown(t, nil, errors.New("boom"))
	code, out, errOut = runCooldownCmd("clear", "glm")
	if code != 1 || out != "" || strings.TrimSpace(errOut) != "smind: boom" {
		t.Fatalf("clear: code %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestCooldownUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"ls", "extra"}} {
		code, out, errOut := runCooldownCmd(args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "smind cooldown ls") || !strings.Contains(errOut, "smind cooldown clear <provider>") {
			t.Errorf("%v: code %d stdout %q stderr %q", args, code, out, errOut)
		}
	}
}
