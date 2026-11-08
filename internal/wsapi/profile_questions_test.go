package wsapi

import (
	"testing"
)

func TestProfileCreate_QuestionsRoundTrip(t *testing.T) {
	t.Parallel()
	ws := newProfileFailoverWS(t)

	created, errMsg := profileCall(t, ws, "a", "profile.create", map[string]any{
		"name": "peer", "provider": "claude-native", "questions": "decline",
	})
	if errMsg != "" {
		t.Fatalf("create: %s", errMsg)
	}
	if created.Questions != "decline" {
		t.Fatalf("created Questions = %q, want decline", created.Questions)
	}
	got, errMsg := profileCall(t, ws, "b", "profile.get", map[string]any{"id": created.ID})
	if errMsg != "" || got.Questions != "decline" {
		t.Fatalf("get = %q, %q; want decline", got.Questions, errMsg)
	}
	updated, errMsg := profileCall(t, ws, "c", "profile.update", map[string]any{
		"id": created.ID, "name": "peer", "provider": "claude-native", "questions": "disabled",
	})
	if errMsg != "" {
		t.Fatalf("update: %s", errMsg)
	}
	if updated.Questions != "disabled" {
		t.Fatalf("updated Questions = %q, want disabled", updated.Questions)
	}
	plain, _ := profileCall(t, ws, "d", "profile.create", map[string]any{"name": "plain", "provider": "claude-native"})
	if plain.Questions != "ask" {
		t.Fatalf("default Questions = %q, want ask", plain.Questions)
	}
	if _, errMsg := profileCall(t, ws, "e", "profile.create", map[string]any{"name": "bad", "provider": "claude-native", "questions": "always"}); errMsg == "" {
		t.Fatalf("bad questions accepted")
	}
}
