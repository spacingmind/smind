package wsapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/plugins/host"
)

// ui.refresh (ADR-0042 section 5 / plan D4): the one push a plugin process
// can ask for. It is a native plugin-session method -- not a wsapi method,
// not in PluginCapabilityTable, no capability needed beyond declaring the
// panel -- because it moves no data: the host publishes plugin.event
// {pluginId, kind: "refresh", panelId?} on the bus and clients refetch. Rate
// limited per plugin so a chatty plugin cannot make the bus its own
// notification channel.

const (
	pluginRPCUIRefresh = "ui.refresh"

	// pluginRefreshesPerSecond bounds ui.refresh per plugin (plan D4).
	pluginRefreshesPerSecond = 4
)

// refreshLimiter is a sliding-window counter per plugin, shaped like
// sendLimiter (plugin_send.go) with an injectable clock for tests.
type refreshLimiter struct {
	mu   sync.Mutex
	now  func() time.Time
	seen map[string][]time.Time
}

func newRefreshLimiter() *refreshLimiter {
	return &refreshLimiter{now: time.Now, seen: map[string][]time.Time{}}
}

// allow records a refresh and reports whether it is under the cap; a refused
// refresh is not recorded, so refusals cost no budget.
func (l *refreshLimiter) allow(plugin string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	ts := trimWindow(l.seen[plugin], now, time.Second)
	if len(ts) >= pluginRefreshesPerSecond {
		l.seen[plugin] = ts
		return false
	}
	l.seen[plugin] = append(ts, now)
	return true
}

// bindPanels installs how ui.refresh checks declarations: declared answers
// "does plugin declare panel", anyPanel "does plugin declare any panel".
// Both are nil until bindPanels, and nil declares nothing (the conservative
// reading: everything is refused).
func (r *pluginRegistry) bindPanels(declared func(plugin, panel string) bool, anyPanel func(plugin string) bool) {
	r.mu.Lock()
	r.panelDeclared, r.panelAny = declared, anyPanel
	r.mu.Unlock()
}

// panelDeclarations derives ui.refresh's two checks from the Manager: a
// panel is declared while its plugin is active and within ui level 1, so a
// disabled plugin's refreshes stop with its panels. This is what the daemon
// passes to bindPanels (server.go).
func panelDeclarations(mgr *plugins.Manager) (declared func(plugin, panel string) bool, anyPanel func(plugin string) bool) {
	declared = func(plugin, panel string) bool {
		panels, _ := mgr.Panels(1)
		for _, p := range panels {
			if p.Plugin == plugin && p.ID == panel {
				return true
			}
		}
		return false
	}
	anyPanel = func(plugin string) bool {
		panels, _ := mgr.Panels(1)
		for _, p := range panels {
			if p.Plugin == plugin {
				return true
			}
		}
		return false
	}
	return declared, anyPanel
}

// refresh resolves ui.refresh. A named panel must be declared; an unnamed
// refresh needs at least one declared panel. See the file comment.
func (s *pluginSession) refresh(params json.RawMessage) (any, *host.RPCError) {
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage("{}")
	}
	var p struct {
		PanelID string `json:"panelId"`
	}
	dec := json.NewDecoder(bytes.NewReader(params))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, rpcErr(host.ErrCodeInvalidParams, "ui.refresh: invalid params", nil)
	}
	s.reg.mu.RLock()
	declared, anyPanel, lim, bus := s.reg.panelDeclared, s.reg.panelAny, s.reg.uiRefresh, s.reg.bus
	s.reg.mu.RUnlock()
	if p.PanelID != "" {
		if declared == nil || !declared(s.id, p.PanelID) {
			return nil, rpcErr(host.ErrCodeInvalidParams, fmt.Sprintf("panel %s is not declared by this plugin", p.PanelID), nil)
		}
	} else if anyPanel == nil || !anyPanel(s.id) {
		return nil, rpcErr(host.ErrCodeInvalidParams, "ui.refresh: this plugin declares no panel", nil)
	}
	if lim == nil || !lim.allow(s.id) {
		return nil, rpcErr(host.ErrCodeBusy, fmt.Sprintf("ui.refresh is limited to %d per second", pluginRefreshesPerSecond), nil)
	}
	if bus == nil {
		return nil, rpcErr(host.ErrCodeInternal, "the session is not available", nil)
	}
	bus.Publish(Event{Topic: TopicPluginEvent, Payload: pluginEventPayload{PluginID: s.id, Kind: "refresh", PanelID: p.PanelID}})
	return map[string]bool{"ok": true}, nil
}
