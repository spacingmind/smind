package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestServer_ProviderList_RoundTrip proves provider.list's wire shape --
// a no-params request producing {providers: [{id, label}]} -- round-trips
// over a real WebSocket connection, returning every provider
// taskrunner.SupportedProviders declares (the single source of truth
// RunPrompt's dispatch stays in sync with).
func TestServer_ProviderList_RoundTrip(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")

	sendRequest(t, ws, "1", "provider.list", nil)
	resp := readEnvelopeFor(t, ws, "1", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("provider.list error = %v", resp.Error.Message)
	}
	var result providerListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode provider.list result: %v", err)
	}

	want := map[taskrunner.Provider]string{
		"claude-native": "Claude Code",
		"glm":           "GLM",
		"kimi":          "Kimi",
		"codex-native":  "Codex",
	}
	if len(result.Providers) != len(want) {
		t.Fatalf("provider.list returned %d providers, want %d: %+v", len(result.Providers), len(want), result.Providers)
	}
	for _, p := range result.Providers {
		label, ok := want[p.ID]
		if !ok {
			t.Fatalf("provider.list returned unknown provider id %q", p.ID)
		}
		if p.Label != label {
			t.Fatalf("provider %q label = %q, want %q", p.ID, p.Label, label)
		}
		delete(want, p.ID)
	}
}
