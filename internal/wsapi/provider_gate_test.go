package wsapi

import (
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/gate"
	"github.com/spacingmind/smind/internal/routing"
	"github.com/spacingmind/smind/internal/runs/failure"
)

func listProviders(t *testing.T, srv *httptest.Server, id string) []map[string]json.RawMessage {
	t.Helper()
	ws := dialWS(t, srv, "tok")
	sendRequest(t, ws, id, "provider.list", nil)
	resp := readEnvelopeFor(t, ws, id, 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("provider.list error = %v", resp.Error.Message)
	}
	var raw struct {
		Providers []map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(resp.Result, &raw); err != nil {
		t.Fatal(err)
	}
	return raw.Providers
}

func providerNamed(t *testing.T, ps []map[string]json.RawMessage, id string) map[string]json.RawMessage {
	t.Helper()
	for _, p := range ps {
		var got string
		_ = json.Unmarshal(p["id"], &got)
		if got == id {
			return p
		}
	}
	t.Fatalf("provider %q not listed", id)
	return nil
}

func newGateAPI(t *testing.T) (*API, *httptest.Server, func(provider string, until time.Time)) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	api, err := New(wm, newTestAccountsRegistry(t), runner, db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	return api, srv, func(provider string, until time.Time) {
		if err := db.SetProviderCooldown(provider, until, "r", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProviderList_CarriesGateAndModelGates(t *testing.T) {
	t.Parallel()
	api, srv, cool := newGateAPI(t)

	for _, p := range listProviders(t, srv, "1") {
		if _, ok := p["gate"]; ok {
			t.Fatalf("gate present with nothing gated: %v", p["id"])
		}
		if _, ok := p["modelGates"]; ok {
			t.Fatalf("modelGates present with nothing gated: %v", p["id"])
		}
	}

	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	cool("glm", until)
	var g gate.Gate
	if err := json.Unmarshal(providerNamed(t, listProviders(t, srv, "2"), "glm")["gate"], &g); err != nil {
		t.Fatalf("glm gate: %v", err)
	}
	if g.Reason != gate.ReasonCoolingDown || g.Until == nil || !g.Until.Equal(until) || g.Action == nil || g.Action.Kind != failure.ActionClearCooldown {
		t.Fatalf("glm gate = %+v", g)
	}
	if _, ok := providerNamed(t, listProviders(t, srv, "3"), "claude-native")["gate"]; ok {
		t.Fatal("claude-native gated by glm's cooldown")
	}

	// A model that just failed outside the plan gates that model only.
	info := api.runner.ProviderCatalog()
	var model string
	for _, p := range info {
		if string(p.ID) == "claude-native" && len(p.Models) > 0 {
			model = p.Models[0].ID
		}
	}
	if model == "" {
		t.Fatal("claude-native has no catalog model")
	}
	api.gate.onFailure("claude-native", model, failure.KindModelNotInPlan)
	var mg map[string]gate.Gate
	cn := providerNamed(t, listProviders(t, srv, "4"), "claude-native")
	if err := json.Unmarshal(cn["modelGates"], &mg); err != nil {
		t.Fatalf("modelGates: %v", err)
	}
	if len(mg) != 1 || mg[model].Reason != gate.ReasonModelNotInPlan || !mg[model].Advisory {
		t.Fatalf("modelGates = %+v", mg)
	}
	if _, ok := cn["gate"]; ok {
		t.Fatal("a model failure gated the provider")
	}
}

type fakeWindowReader struct{ v routing.Verdict }

func (f fakeWindowReader) Exhaustion(int64, string, time.Time) routing.Verdict { return f.v }

func TestProviderList_Gates_OutOfPlanFromCachedWindowsIsAdvisory(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	acctReg := newTestAccountsRegistry(t)
	api, err := New(wm, acctReg, newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	if _, err := acctReg.AddAPIKey("anthropic", "main", "sk-ant-x"); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	api.AttachGateWindows(fakeWindowReader{routing.Verdict{Exhausted: true, Until: reset}})
	var g gate.Gate
	if err := json.Unmarshal(providerNamed(t, listProviders(t, srv, "1"), "claude-native")["gate"], &g); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if g.Reason != gate.ReasonOutOfPlan || !g.Advisory || g.Until == nil || !g.Until.Equal(reset) {
		t.Fatalf("gate = %+v", g)
	}
	// No account for the provider: nothing known, nothing gated (runs use the CLI login).
	if _, ok := providerNamed(t, listProviders(t, srv, "2"), "codex-native")["gate"]; ok {
		t.Fatal("codex-native gated without an account")
	}
}

func TestProviderUpdated_PublishedOnGateEdges(t *testing.T) {
	t.Parallel()
	api, _, cool := newGateAPI(t)
	var mu sync.Mutex
	n := 0
	api.bus.setTap(func(e Event) {
		if e.Topic == TopicProviderUpdated {
			mu.Lock()
			n++
			mu.Unlock()
		}
	})
	count := func() int { mu.Lock(); defer mu.Unlock(); return n }

	api.gate.notifyIfChanged() // establishes the baseline (no gates): no event
	base := count()
	api.gate.notifyIfChanged()
	if count() != base {
		t.Fatal("event published with no change")
	}
	cool("glm", time.Now().Add(time.Hour))
	api.gate.notifyIfChanged()
	if count() != base+1 {
		t.Fatalf("events = %d, want one for the cooldown edge", count()-base)
	}
	api.gate.notifyIfChanged()
	if count() != base+1 {
		t.Fatal("unchanged gate re-published (not coalesced)")
	}
	api.gate.onFailure("kimi", "", failure.KindAuthExpired)
	if count() != base+2 {
		t.Fatalf("events = %d, want one for the auth failure edge", count()-base)
	}
}

func TestProviderGate_AccountChangeClearsRecentFailures(t *testing.T) {
	t.Parallel()
	wm, db := newTestWorkspaceManager(t)
	acctReg := newTestAccountsRegistry(t)
	api, err := New(wm, acctReg, newTestRunner(wm), db, "tok")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	acct, err := acctReg.AddAPIKey("kimi", "k", "sk-k")
	if err != nil {
		t.Fatal(err)
	}
	api.gate.onFailure("kimi", "", failure.KindAuthExpired)
	if g := api.gate.deriveFn("kimi", time.Now()); g == nil || g.Reason != gate.ReasonNotSignedIn {
		t.Fatalf("gate = %+v, want not_signed_in after an auth failure", g)
	}
	if _, err := acctReg.ReplaceCredential(acct.ID, "sk-new", ""); err != nil {
		t.Fatal(err)
	}
	if g := api.gate.deriveFn("kimi", time.Now()); g != nil {
		t.Fatalf("gate = %+v, want cleared by the credential change", g)
	}
}
