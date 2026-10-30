package store

import (
	"testing"
	"time"
)

func TestStore_ClearProviderCooldown(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.SetProviderCooldown("glm", time.Now().Add(time.Hour).UTC(), "earlier", time.Now()); err != nil {
		t.Fatalf("SetProviderCooldown() error = %v", err)
	}
	cleared, err := s.ClearProviderCooldown("glm")
	if err != nil || !cleared {
		t.Fatalf("ClearProviderCooldown() = %v, %v, want true", cleared, err)
	}
	if _, ok, err := s.GetProviderCooldown("glm", time.Now()); err != nil || ok {
		t.Fatalf("GetProviderCooldown() = %v, %v, want gone", ok, err)
	}
	cleared, err = s.ClearProviderCooldown("glm")
	if err != nil || cleared {
		t.Fatalf("second ClearProviderCooldown() = %v, %v, want false", cleared, err)
	}
}

func TestStore_ReleaseHeldRetries_OnlyMatchingProvider(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	a := newQueueChat(t, s)
	b := newQueueChat(t, s)
	held := time.Now().Add(time.Hour).UTC()
	glmRetry := `{"provider":"glm","failoverOf":"r1"}`
	enqueueAtConfig := func(chatID int64, prompt, runConfig, source string, nb time.Time) ChatQueueItem {
		t.Helper()
		item, err := s.EnqueueChatPromptAt(chatID, prompt, runConfig, source, 0, 0, 0, "", &nb)
		if err != nil {
			t.Fatalf("EnqueueChatPromptAt(%s) error = %v", prompt, err)
		}
		return item
	}
	glmA := enqueueAtConfig(a.ID, "glm-a", glmRetry, ChatQueueSourceOrchestrator, held)
	glmB := enqueueAtConfig(b.ID, "glm-b", glmRetry, ChatQueueSourceOrchestrator, held)
	claude := enqueueAtConfig(a.ID, "claude", `{"provider":"claude","failoverOf":"x"}`, ChatQueueSourceOrchestrator, held)
	human := enqueueAtConfig(a.ID, "human", `{"provider":"glm","failoverOf":"r1"}`, ChatQueueSourceHuman, held)
	noFailover := enqueueAtConfig(b.ID, "plain", `{"provider":"glm"}`, ChatQueueSourceOrchestrator, held)

	ids, err := s.ReleaseHeldRetries("glm", time.Now())
	if err != nil {
		t.Fatalf("ReleaseHeldRetries() error = %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("released = %v, want the two glm retry chat ids", ids)
	}
	want := map[int64]bool{a.ID: true, b.ID: true}
	for _, id := range ids {
		if !want[id] {
			t.Fatalf("released chat id %d, want %v", id, want)
		}
	}
	for id, item := range map[int64]ChatQueueItem{glmA.ID: glmA, glmB.ID: glmB} {
		got, err := s.GetChatQueueItem(item.ID)
		if err != nil || got.NotBefore != nil {
			t.Fatalf("released item %d NotBefore = %v, %v, want nil", id, got.NotBefore, err)
		}
	}
	for _, item := range []ChatQueueItem{claude, human, noFailover} {
		got, err := s.GetChatQueueItem(item.ID)
		if err != nil || got.NotBefore == nil || !got.NotBefore.Equal(held) {
			t.Fatalf("item %d NotBefore = %v, %v, want the original hold", item.ID, got.NotBefore, err)
		}
	}

	if ids, err := s.ReleaseHeldRetries("glm", time.Now()); err != nil || len(ids) != 0 {
		t.Fatalf("second ReleaseHeldRetries() = %v, %v, want none", ids, err)
	}
}
