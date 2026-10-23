package main

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/store"
)

func TestProfileAdd_OnRateLimitFlags(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	target, err := reg.Create(store.AgentProfile{Name: "target", Provider: "glm"})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	id := strconv.FormatInt(target.ID, 10)

	var code int
	captureStdout(t, func() {
		code = run([]string{"profile", "add", "main", "claude-native", "--on-rate-limit=fallback", "--fallback-profile=" + id})
	})
	if code != 0 {
		t.Fatalf("run(profile add) = %d, want 0", code)
	}
	list, _ := reg.List()
	if len(list) != 2 {
		t.Fatalf("profiles = %+v", list)
	}
	got := list[1]
	if got.OnRateLimit != "fallback" || got.FallbackProfileID == nil || *got.FallbackProfileID != target.ID {
		t.Fatalf("stored = %q / %v", got.OnRateLimit, got.FallbackProfileID)
	}

	stderr := captureStderr(t, func() {
		code = run([]string{"profile", "add", "bad", "claude-native", "--on-rate-limit", "bogus"})
	})
	if code == 0 {
		t.Fatalf("bogus --on-rate-limit accepted; stderr %q", stderr)
	}
	stderr = captureStderr(t, func() {
		code = run([]string{"profile", "add", "bad", "claude-native", "--fallback-profile=abc"})
	})
	if code != 2 {
		t.Fatalf("non-numeric --fallback-profile = %d, want 2; stderr %q", code, stderr)
	}
}

func TestProfileLs_ShowsOnRateLimit(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	target, _ := reg.Create(store.AgentProfile{Name: "target", Provider: "glm"})
	if code := run([]string{"profile", "add", "waiter", "claude-native", "--on-rate-limit=wait"}); code != 0 {
		t.Fatalf("add waiter = %d", code)
	}
	if code := run([]string{"profile", "add", "falls", "claude-native", "--on-rate-limit=fallback", "--fallback-profile=" + strconv.FormatInt(target.ID, 10)}); code != 0 {
		t.Fatalf("add falls = %d", code)
	}

	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "ls"}) })
	if code != 0 {
		t.Fatalf("profile ls = %d", code)
	}
	for _, want := range []string{"ON-RATE-LIMIT", "none", "wait", "fallback→" + strconv.FormatInt(target.ID, 10)} {
		if !strings.Contains(out, want) {
			t.Errorf("profile ls output missing %q:\n%s", want, out)
		}
	}
}

func TestMCPProfileList_FailoverFields(t *testing.T) {
	env := newMCPSession(t)
	client := dialTestClient(t, env.srvURL)

	var target struct {
		ID int64 `json:"ID"`
	}
	if err := client.Call(context.Background(), "profile.create", map[string]any{"name": "target", "provider": "glm"}, &target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := client.Call(context.Background(), "profile.create", map[string]any{
		"name": "main", "provider": "claude-native", "onRateLimit": "fallback", "fallbackProfileId": target.ID,
	}, nil); err != nil {
		t.Fatalf("create main: %v", err)
	}

	var out struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if isErr := callMCPTool(t, env.cs, "profile_list", map[string]any{}, &out); isErr {
		t.Fatal("profile_list returned an error result")
	}
	if len(out.Profiles) != 2 {
		t.Fatalf("profiles = %+v", out.Profiles)
	}
	plain, main := out.Profiles[0], out.Profiles[1]
	if _, ok := plain["onRateLimit"]; ok {
		t.Errorf("plain profile has onRateLimit: %+v", plain)
	}
	if _, ok := plain["fallbackProfileId"]; ok {
		t.Errorf("plain profile has fallbackProfileId: %+v", plain)
	}
	if main["onRateLimit"] != "fallback" || main["fallbackProfileId"] != float64(target.ID) {
		t.Errorf("main profile = %+v, want onRateLimit fallback / fallbackProfileId %d", main, target.ID)
	}
}
