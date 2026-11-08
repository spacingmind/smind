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

func TestProfileAdd_QuestionsFlag(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	var code int
	captureStdout(t, func() { code = run([]string{"profile", "add", "quiet", "claude-native", "--questions=decline"}) })
	if code != 0 {
		t.Fatalf("add = %d", code)
	}
	captureStdout(t, func() { code = run([]string{"profile", "add", "plain", "claude-native"}) })
	list, _ := reg.List()
	got := map[string]string{}
	for _, p := range list {
		got[p.Name] = p.Questions
	}
	if got["quiet"] != "decline" || got["plain"] != "ask" {
		t.Fatalf("stored questions = %v", got)
	}
	stderr := captureStderr(t, func() { code = run([]string{"profile", "add", "bad", "claude-native", "--questions=sometimes"}) })
	if code == 0 {
		t.Fatalf("invalid --questions accepted; stderr %q", stderr)
	}
}

// An edit that does not name --questions carries the current value explicitly,
// so a profile that declines never silently goes back to asking; naming it changes it.
func TestProfileEdit_QuestionsFlagAndCarry(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	p, err := reg.Create(store.AgentProfile{Name: "quiet", Provider: "claude-native", Questions: "decline"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(p.ID, 10)
	var code int
	captureStdout(t, func() { code = run([]string{"profile", "edit", id, "--notes=changed"}) })
	if code != 0 {
		t.Fatalf("edit = %d", code)
	}
	got, _ := reg.Get(p.ID)
	if got.Questions != "decline" || got.Notes != "changed" {
		t.Fatalf("after notes-only edit: questions=%q notes=%q", got.Questions, got.Notes)
	}
	captureStdout(t, func() { code = run([]string{"profile", "edit", id, "--questions=disabled"}) })
	got, _ = reg.Get(p.ID)
	if code != 0 || got.Questions != "disabled" {
		t.Fatalf("edit --questions=disabled: code %d, stored %q", code, got.Questions)
	}
}

func TestProfileLs_ShowsQuestions(t *testing.T) {
	_, s := newTestProfileDaemon(t)
	reg := profiles.New(s)
	_, _ = reg.Create(store.AgentProfile{Name: "quiet", Provider: "claude-native", Questions: "decline"})
	var code int
	out := captureStdout(t, func() { code = run([]string{"profile", "ls"}) })
	if code != 0 || !strings.Contains(out, "QUESTIONS") || !strings.Contains(out, "decline") {
		t.Fatalf("profile ls (code %d) = %q", code, out)
	}
}

func TestAddProfileFailoverFields_ShowsQuestionsUnlessDefault(t *testing.T) {
	p := map[string]any{"Questions": "decline"}
	addProfileFailoverFields(p)
	if p["questions"] != "decline" {
		t.Fatalf("questions not shown: %v", p)
	}
	q := map[string]any{"Questions": "ask"}
	addProfileFailoverFields(q)
	if _, ok := q["questions"]; ok {
		t.Fatalf("the default is shown: %v", q)
	}
}
