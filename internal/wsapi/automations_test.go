package wsapi

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/store"
)

type autoHarness struct {
	db    *store.Store
	hs    map[string]handlerFunc
	ws    store.Workspace
	prof  store.AgentProfile
	bus   *eventBus
	allow *bool
}

func newAutoHarness(t *testing.T) *autoHarness {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ws, _ := db.CreateWorkspace(store.Workspace{Path: "/tmp/w", Title: "w"})
	prof, _ := db.CreateAgentProfile(store.AgentProfile{Name: "lead", Provider: "claude-native", PermissionMode: "default"})
	allow := false
	old := configAutomations
	configAutomations = func() config.AutomationsConfig {
		return config.AutomationsConfig{AllowRemoteManage: allow, MaxBudgetUSD: 10}
	}
	t.Cleanup(func() { configAutomations = old })
	bus := newEventBus()
	return &autoHarness{db: db, hs: automationHandlers(newAutomationHost(db, bus, home)), ws: ws, prof: prof, bus: bus, allow: &allow}
}

func (h *autoHarness) call(method string, rc *requestContext, params any) (any, error) {
	raw, _ := json.Marshal(params)
	return h.hs[method](context.Background(), rc, raw)
}

func (h *autoHarness) def() map[string]any {
	return map[string]any{
		"name": "pilot", "workspaceId": h.ws.ID,
		"signal":   map[string]any{"kind": "github.workflow_run", "workflow": "Windows full suite"},
		"template": map[string]any{"profileId": h.prof.ID, "model": "claude-sonnet-5-5", "mode": "default", "titleTemplate": "t", "promptTemplate": "p {{signal.runId}}"},
	}
}

func (h *autoHarness) paramsFor(method string, id int64) any {
	switch method {
	case "automation.create":
		d := h.def()
		d["name"] = "other"
		return d
	case "automation.update":
		d := h.def()
		d["id"] = id
		return d
	default:
		return map[string]any{"id": id}
	}
}

func wantAutoForbidden(t *testing.T, label string, err error) {
	t.Helper()
	ce, ok := err.(*rpcCodeError)
	if !ok || ce.RPCCode() != codeForbidden {
		t.Fatalf("%s: want forbidden, got %v", label, err)
	}
}

func local() *requestContext { return &requestContext{local: true, channel: ChannelLoopback} }

func TestAutomationRPC_RefusedForRunScoped_NilAndAutomationOriginTask(t *testing.T) {
	h := newAutoHarness(t)
	a, err := h.call("automation.create", local(), h.def())
	if err != nil {
		t.Fatal(err)
	}
	id := a.(automationView).ID
	// A run-scoped connection is what an agent -- and so a task an automation
	// started -- gets from session.identify.
	agent := &requestContext{runScoped: true, local: true, channel: ChannelLoopback}
	for _, m := range append(append([]string{}, automationMutating...), automationBrake...) {
		_, err := h.call(m, agent, h.paramsFor(m, id))
		wantAutoForbidden(t, m, err)
		_, err = h.call(m, nil, h.paramsFor(m, id))
		wantAutoForbidden(t, m+" (nil rc)", err)
	}
	if got, _ := h.db.GetAutomation(id); got.Enabled {
		t.Fatal("an agent enabled an automation")
	}
	if rows, _ := h.db.ListAutomations(); len(rows) != 1 {
		t.Fatalf("an agent created or deleted an automation: %d rows", len(rows))
	}
}

func TestAutomationRPC_NotInRunScopedMethods(t *testing.T) {
	for _, m := range append(append(append([]string{}, automationMutating...), automationBrake...), automationReads...) {
		if runScopedSet[m] {
			t.Errorf("%s is on RunScopedMethods", m)
		}
	}
	for _, m := range RunScopedMethods {
		if strings.HasPrefix(m, "automation.") {
			t.Errorf("RunScopedMethods has %s", m)
		}
	}
}

func TestAutomationRPC_RelayMutationsRefused_DisableAndReadsAllowed(t *testing.T) {
	h := newAutoHarness(t)
	a, _ := h.call("automation.create", local(), h.def())
	id := a.(automationView).ID
	if _, err := h.call("automation.enable", local(), map[string]any{"id": id}); err != nil {
		t.Fatal(err)
	}
	// The third context is a relay session whose channel claims loopback: the
	// local flag alone must refuse it.
	for _, relay := range []*requestContext{{local: false, deviceID: "d1", channel: ChannelE2EE}, {local: false}, {local: false, channel: ChannelLoopback}} {
		for _, m := range automationMutating {
			_, err := h.call(m, relay, h.paramsFor(m, id))
			wantAutoForbidden(t, m, err)
		}
		for _, m := range []string{"automation.list", "automation.get", "automation.audit", "automation.memory.get"} {
			if _, err := h.call(m, relay, map[string]any{"id": id}); err != nil {
				t.Errorf("%s over the relay: %v", m, err)
			}
		}
	}
	relay := &requestContext{local: false, deviceID: "d1", channel: ChannelE2EE}
	if _, err := h.call("automation.disable", relay, map[string]any{"id": id}); err != nil {
		t.Fatalf("relay disable (a brake) refused: %v", err)
	}
	if got, _ := h.db.GetAutomation(id); got.Enabled {
		t.Fatal("relay disable did not disable")
	}
	// A relay session cannot arm it again.
	_, err := h.call("automation.enable", relay, map[string]any{"id": id})
	wantAutoForbidden(t, "relay enable", err)
}

func TestAutomationRPC_NonLoopbackNeedsAllowRemote(t *testing.T) {
	h := newAutoHarness(t)
	lan := &requestContext{local: true, channel: ChannelPlain}
	_, err := h.call("automation.create", lan, h.def())
	wantAutoForbidden(t, "plain non-loopback create", err)
	*h.allow = true
	if _, err := h.call("automation.create", lan, h.def()); err != nil {
		t.Fatalf("allowRemoteManage create: %v", err)
	}
}

func TestAutomationRPC_CreateStartsDisabled_AndRoundTrip(t *testing.T) {
	h := newAutoHarness(t)
	v, err := h.call("automation.create", local(), h.def())
	if err != nil {
		t.Fatal(err)
	}
	a := v.(automationView)
	if a.Enabled || a.Status != "active" || a.Rails.MaxRunsPerHour != 2 || a.Template.BudgetUSD != 3 {
		t.Fatalf("created = %+v", a)
	}
	got, err := h.call("automation.get", local(), map[string]any{"id": a.ID})
	if err != nil || got.(automationView).Name != "pilot" {
		t.Fatalf("get = %v %v", got, err)
	}
	l, _ := h.call("automation.list", local(), nil)
	if n := len(l.(struct {
		Enabled     bool             `json:"enabled"`
		Paused      bool             `json:"paused"`
		Automations []automationView `json:"automations"`
	}).Automations); n != 1 {
		t.Fatalf("list = %d", n)
	}
	bad := h.def()
	bad["template"].(map[string]any)["mode"] = "full-access"
	bad["name"] = "bad"
	if _, err := h.call("automation.create", local(), bad); err == nil || !strings.Contains(err.Error(), "allowUnattendedModes") {
		t.Fatalf("unattended mode accepted: %v", err)
	}
	if _, err := h.call("automation.create", local(), h.def()); err == nil {
		t.Fatal("duplicate name accepted")
	}
}

func TestAutomationRPC_AuditAndFireUnavailableWithoutEngine(t *testing.T) {
	h := newAutoHarness(t)
	a, _ := h.call("automation.create", local(), h.def())
	id := a.(automationView).ID
	r, err := h.call("automation.audit", local(), map[string]any{"id": id})
	if err != nil || len(r.(map[string]any)["rows"].([]automationAuditView)) != 0 {
		t.Fatalf("audit = %v %v", r, err)
	}
	_, err = h.call("automation.fire", local(), map[string]any{"id": id})
	if ce, ok := err.(*rpcCodeError); !ok || ce.RPCCode() != codeUnavailable {
		t.Fatalf("fire without an engine: %v", err)
	}
	if _, err := h.call("automation.get", local(), map[string]any{"id": 999}); err == nil {
		t.Fatal("unknown id")
	}
}

func TestAutomationEvents_Topics(t *testing.T) {
	for _, topic := range []string{TopicAutomationFired, TopicAutomationPaused, TopicAutomationUpdated} {
		if !knownTopics[topic] {
			t.Errorf("%s missing from knownTopics", topic)
		}
	}
}
