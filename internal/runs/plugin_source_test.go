package runs

import (
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

func TestDeliverPrompt_PluginSourceHeader(t *testing.T) {
	item := store.ChatQueueItem{Source: store.PluginSource("acme"), Prompt: "do it"}
	if got, want := deliverPrompt(item), "[message from plugin acme]\ndo it"; got != want {
		t.Fatalf("deliverPrompt = %q, want %q", got, want)
	}
	// A human item stays verbatim; "plugin:" with no id is not a plugin source.
	if got := deliverPrompt(store.ChatQueueItem{Source: store.ChatQueueSourceHuman, Prompt: "hi"}); got != "hi" {
		t.Fatalf("human = %q", got)
	}
	if got := deliverPrompt(store.ChatQueueItem{Source: "plugin:", Prompt: "hi"}); got != "hi" {
		t.Fatalf("empty id = %q", got)
	}
}

func TestBudgetedOrigin_AgentAndPlugin(t *testing.T) {
	for origin, want := range map[string]bool{
		store.ChatQueueSourceAgent: true, store.PluginSource("acme"): true,
		store.ChatQueueSourceHuman: false, store.ChatQueueSourceOrchestrator: false, store.ChatQueueSourceChildEvent: false, "": false, "plugin:": false,
	} {
		if got := budgetedOrigin(origin); got != want {
			t.Errorf("budgetedOrigin(%q) = %v, want %v", origin, got, want)
		}
	}
}
