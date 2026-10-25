package wsapi

import (
	"context"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/plugins"
	"github.com/spacingmind/smind/internal/store"
)

// TopicPluginUpdated is published whenever a plugin's state changed: an
// install, enable, disable, remove, update or reload, or a flip of the global
// plugins.enabled switch. Payload {id, globalEnabled}; id is "" for a switch
// flip. Clients refetch plugin.list, mcp.list, profile.list and skill.list.
const TopicPluginUpdated = "plugin.updated"

// pluginUpdatedPayload is the payload of plugin.updated events.
type pluginUpdatedPayload struct {
	ID            string `json:"id"`
	GlobalEnabled bool   `json:"globalEnabled"`
}

// pluginEvents turns a change in plugin state into the events clients already
// understand: mcpServer.created/deleted and profile.created/deleted for each
// owned row that appeared or vanished (so existing lists refetch), then one
// plugin.updated.
type pluginEvents struct {
	bus *eventBus
	mgr *plugins.Manager

	mu     sync.Mutex
	mcp    map[int64]store.McpServer
	prof   map[int64]store.AgentProfile
	global bool
}

func newPluginEvents(bus *eventBus, mgr *plugins.Manager) *pluginEvents {
	e := &pluginEvents{bus: bus, mgr: mgr}
	e.mcp, e.prof, e.global = e.snapshot()
	return e
}

func (e *pluginEvents) snapshot() (map[int64]store.McpServer, map[int64]store.AgentProfile, bool) {
	mcp, prof := map[int64]store.McpServer{}, map[int64]store.AgentProfile{}
	for _, m := range e.mgr.OwnedMcp() {
		mcp[m.ID] = m
	}
	for _, p := range e.mgr.OwnedProfiles() {
		prof[p.ID] = p
	}
	return mcp, prof, e.mgr.GlobalEnabled()
}

// changed publishes the diff since the last call and plugin.updated for id.
func (e *pluginEvents) changed(id string) {
	e.mu.Lock()
	oldMcp, oldProf := e.mcp, e.prof
	e.mcp, e.prof, e.global = e.snapshot()
	newMcp, newProf, global := e.mcp, e.prof, e.global
	e.mu.Unlock()

	for mid, m := range newMcp {
		switch old, ok := oldMcp[mid]; {
		case !ok:
			e.bus.Publish(Event{Topic: TopicMcpServerCreated, Payload: mcpServerCreatedPayload{Server: mcpServerResultFrom(m)}})
		case !old.UpdatedAt.Equal(m.UpdatedAt):
			e.bus.Publish(Event{Topic: TopicMcpServerUpdated, Payload: mcpServerUpdatedPayload{Server: mcpServerResultFrom(m)}})
		}
	}
	for mid := range oldMcp {
		if _, ok := newMcp[mid]; !ok {
			e.bus.Publish(Event{Topic: TopicMcpServerDeleted, Payload: mcpServerDeletedPayload{ID: mid}})
		}
	}
	for pid, p := range newProf {
		switch old, ok := oldProf[pid]; {
		case !ok:
			e.bus.Publish(Event{Topic: TopicProfileCreated, Payload: profileCreatedPayload{Profile: p}})
		case !old.UpdatedAt.Equal(p.UpdatedAt):
			e.bus.Publish(Event{Topic: TopicProfileUpdated, Payload: profileUpdatedPayload{Profile: p}})
		}
	}
	for pid := range oldProf {
		if _, ok := newProf[pid]; !ok {
			e.bus.Publish(Event{Topic: TopicProfileDeleted, Payload: profileDeletedPayload{ID: pid}})
		}
	}
	e.bus.Publish(Event{Topic: TopicPluginUpdated, Payload: pluginUpdatedPayload{ID: id, GlobalEnabled: global}})
}

// watch notices a flip of plugins.enabled (a config.yaml edit has no daemon
// hook) and announces it like any plugin change. It stops with ctx.
func (e *pluginEvents) watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.mu.Lock()
			flipped := e.mgr.GlobalEnabled() != e.global
			e.mu.Unlock()
			if flipped {
				e.changed("")
			}
		}
	}
}

// WatchPlugins starts the global-switch watcher; the returned func stops it.
func (a *API) WatchPlugins(ctx context.Context) func() {
	if a.pluginEvents == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); a.pluginEvents.watch(ctx, 2*time.Second) }()
	return func() { cancel(); <-done }
}
