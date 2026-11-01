// subscription.ts: the events the plugin screens listen to. plugin.updated fires for any plugin state change (and
// for a flip of the global switch, id ""), so every plugin read (list, detail, commands, themes) refetches on it.
// plugin.event is a plugin process asking clients to refetch one panel (a ui.refresh), so the panel refetches.

import { RelayConnection, RelayEvent } from '../relay/RelayConnection';
import { PLUGIN_EVENT_TOPIC, PluginEventPayload, PLUGIN_UPDATED_TOPIC, PluginUpdatedPayload } from './types';

/** Calls onUpdated for every plugin.updated event; returns the unsubscribe. */
export function watchPluginUpdates(conn: RelayConnection, onUpdated: (p: PluginUpdatedPayload) => void): () => void {
  return conn.subscribe([PLUGIN_UPDATED_TOPIC], (event: RelayEvent) => {
    const p = event.payload as Partial<PluginUpdatedPayload> | null;
    onUpdated({ id: typeof p?.id === 'string' ? p.id : '', globalEnabled: p?.globalEnabled === true });
  });
}

/** Calls onEvent for every plugin.event (a plugin asked clients to refetch a panel); returns the unsubscribe. */
export function watchPluginEvents(conn: RelayConnection, onEvent: (p: PluginEventPayload) => void): () => void {
  return conn.subscribe([PLUGIN_EVENT_TOPIC], (event: RelayEvent) => {
    const p = event.payload as Partial<PluginEventPayload> | null;
    const out: PluginEventPayload = { pluginId: typeof p?.pluginId === 'string' ? p.pluginId : '', kind: typeof p?.kind === 'string' ? p.kind : '' };
    if (typeof p?.panelId === 'string') out.panelId = p.panelId;
    onEvent(out);
  });
}
