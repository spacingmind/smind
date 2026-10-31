package wsapi

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/plugins/host"
)

// refreshHarness is a bound registry whose bus events are recorded, with a
// fake clock on the ui.refresh limiter and panels p1/p2 declared by "demo".
type refreshHarness struct {
	reg    *pluginRegistry
	mu     sync.Mutex
	events []Event
	now    time.Time
}

func newRefreshHarness(t *testing.T) *refreshHarness {
	t.Helper()
	h := &refreshHarness{now: time.Unix(1_700_000_000, 0)}
	bus := newEventBus()
	bus.setTap(func(e Event) {
		h.mu.Lock()
		h.events = append(h.events, e)
		h.mu.Unlock()
	})
	h.reg = newPluginRegistry()
	h.reg.bind(map[string]handlerFunc{}, bus)
	h.reg.uiRefresh.now = func() time.Time { return h.now }
	h.reg.bindPanels(
		func(plugin, panel string) bool { return plugin == "demo" && (panel == "p1" || panel == "p2") },
		func(plugin string) bool { return plugin == "demo" },
	)
	return h
}

func (h *refreshHarness) session(id string) host.Session {
	return h.reg.Open(host.Plugin{ID: id}, func(string, any) error { return nil })
}

func (h *refreshHarness) published() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Event(nil), h.events...)
}

func refreshCall(s host.Session, params string) (any, *host.RPCError) {
	return s.Request(context.Background(), "ui.refresh", json.RawMessage(params))
}

func TestPluginEvent_UiRefresh_PublishesTopic(t *testing.T) {
	h := newRefreshHarness(t)
	s := h.session("demo")
	res, rerr := refreshCall(s, `{"panelId":"p1"}`)
	if rerr != nil || res.(map[string]bool)["ok"] != true {
		t.Fatalf("refresh: %v %v", res, rerr)
	}
	ev := h.published()
	if len(ev) != 1 || ev[0].Topic != TopicPluginEvent {
		t.Fatalf("events: %+v", ev)
	}
	if got := ev[0].Payload.(pluginEventPayload); got != (pluginEventPayload{PluginID: "demo", Kind: "refresh", PanelID: "p1"}) {
		t.Fatalf("payload: %+v", got)
	}
	// No panelId: allowed for a plugin that declares a panel; the field is omitted.
	if _, rerr := refreshCall(s, `{}`); rerr != nil {
		t.Fatalf("unnamed refresh: %v", rerr)
	}
	b, _ := json.Marshal(h.published()[1].Payload)
	if string(b) != `{"pluginId":"demo","kind":"refresh"}` {
		t.Fatalf("unnamed payload: %s", b)
	}
	// Unknown params are invalid.
	if _, rerr := refreshCall(s, `{"panelId":"p1","x":1}`); rerr == nil || rerr.Code != host.ErrCodeInvalidParams {
		t.Fatalf("unknown field: %+v", rerr)
	}
}

func TestPluginEvent_RateLimited(t *testing.T) {
	h := newRefreshHarness(t)
	s := h.session("demo")
	for i := 0; i < 4; i++ {
		if _, rerr := refreshCall(s, `{"panelId":"p1"}`); rerr != nil {
			t.Fatalf("call %d: %v", i+1, rerr)
		}
	}
	if _, rerr := refreshCall(s, `{"panelId":"p1"}`); rerr == nil || rerr.Code != host.ErrCodeBusy {
		t.Fatalf("5th call must be refused: %+v", rerr)
	}
	if n := len(h.published()); n != 4 {
		t.Fatalf("refused call published: %d events", n)
	}
	h.now = h.now.Add(time.Second)
	if _, rerr := refreshCall(s, `{"panelId":"p1"}`); rerr != nil {
		t.Fatalf("after the window: %v", rerr)
	}
}

func TestPluginEvent_UndeclaredPanelRefused(t *testing.T) {
	h := newRefreshHarness(t)
	_, rerr := refreshCall(h.session("demo"), `{"panelId":"nope"}`)
	if rerr == nil || rerr.Message != "panel nope is not declared by this plugin" {
		t.Fatalf("refusal: %+v", rerr)
	}
	// Another plugin's panel id is not this plugin's.
	if _, rerr := refreshCall(h.session("other"), `{"panelId":"p1"}`); rerr == nil {
		t.Fatal("a plugin must not refresh a panel it does not declare")
	}
	if n := len(h.published()); n != 0 {
		t.Fatalf("published %d events", n)
	}
}

func TestPluginEvent_InKnownTopics(t *testing.T) {
	if !knownTopics[TopicPluginEvent] {
		t.Error("plugin.event must be a known topic")
	}
	if contains(PluginEventTopics, TopicPluginEvent) {
		t.Error("plugin.event must not be observable by plugins")
	}
}

func TestPluginAPITable_NoPanelMethod_ForPlugins(t *testing.T) {
	for capName, ms := range PluginCapabilityTable {
		for _, m := range ms {
			switch m.Name {
			case "ui.refresh", "plugin.view", "plugin.panels":
				t.Errorf("%s lists %s", capName, m.Name)
			}
		}
	}
	h := newRefreshHarness(t)
	s := h.session("demo")
	for _, method := range []string{"plugin.view", "plugin.panels"} {
		if _, rerr := s.Request(context.Background(), method, json.RawMessage(`{}`)); rerr == nil || rerr.Code != host.ErrCodeForbidden {
			t.Errorf("%s from a plugin: %+v", method, rerr)
		}
	}
	if _, rerr := refreshCall(h.session("nopanel"), `{}`); rerr == nil {
		t.Error("ui.refresh from a plugin with no panel must be refused")
	}
}
