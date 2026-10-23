package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

// TestChatQueueItem_CarriesNotBefore: a held queue item (ADR-0041 N1) reaches
// clients with its NotBefore (RFC 3339), so the queue strip can say when it
// starts; an item without a hold carries null.
func TestChatQueueItem_CarriesNotBefore(t *testing.T) {
	t.Parallel()
	hold := time.Date(2026, 10, 23, 3, 30, 0, 0, time.UTC)
	items := toChatQueueItemParams([]store.ChatQueueItem{
		{ID: 1, Prompt: "held", Source: store.ChatQueueSourceChildEvent, Status: store.ChatQueueStatusQueued, NotBefore: &hold},
		{ID: 2, Prompt: "plain", Source: store.ChatQueueSourceHuman, Status: store.ChatQueueStatusQueued},
	})
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if got, _ := back[0]["NotBefore"].(string); got != "2026-10-23T03:30:00Z" {
		t.Fatalf("held item NotBefore = %v (json %s), want 2026-10-23T03:30:00Z", back[0]["NotBefore"], raw)
	}
	if v, ok := back[1]["NotBefore"]; !ok || v != nil {
		t.Fatalf("plain item NotBefore = %v (present %v), want null; json %s", v, ok, strings.TrimSpace(string(raw)))
	}
}
