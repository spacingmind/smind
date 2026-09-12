package wsapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestServer_ProviderList_RoundTrip proves provider.list's wire shape --
// a no-params request producing {providers: [{id, label, kind}]} --
// round-trips over a real WebSocket connection, returning every provider
// taskrunner.SupportedProviders declares (the single source of truth
// RunPrompt's dispatch stays in sync with), including GLM's "cli" Kind --
// the signal accounts-dialog.tsx uses to render it as externally-managed
// instead of omitting it or offering a credential form.
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

	type wantInfo struct {
		label string
		kind  taskrunner.ProviderKind
	}
	want := map[taskrunner.Provider]wantInfo{
		"claude-native": {label: "Claude Code"},
		"glm":           {label: "GLM", kind: taskrunner.ProviderKindCLI},
		"kimi":          {label: "Kimi"},
		"codex-native":  {label: "Codex"},
	}
	if len(result.Providers) != len(want) {
		t.Fatalf("provider.list returned %d providers, want %d: %+v", len(result.Providers), len(want), result.Providers)
	}
	for _, p := range result.Providers {
		info, ok := want[p.ID]
		if !ok {
			t.Fatalf("provider.list returned unknown provider id %q", p.ID)
		}
		if p.Label != info.label {
			t.Fatalf("provider %q label = %q, want %q", p.ID, p.Label, info.label)
		}
		if p.Kind != info.kind {
			t.Fatalf("provider %q kind = %q, want %q", p.ID, p.Kind, info.kind)
		}
		delete(want, p.ID)
	}
}
