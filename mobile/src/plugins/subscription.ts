// subscription.ts: the one event the plugin screens listen to. plugin.updated fires for any plugin state change (and
// for a flip of the global switch, id ""), so every plugin read (list, detail, commands, themes) refetches on it.

import { RelayConnection, RelayEvent } from '../relay/RelayConnection';
import { PLUGIN_UPDATED_TOPIC, PluginUpdatedPayload } from './types';

/** Calls onUpdated for every plugin.updated event; returns the unsubscribe. */
export function watchPluginUpdates(conn: RelayConnection, onUpdated: (p: PluginUpdatedPayload) => void): () => void {
  return conn.subscribe([PLUGIN_UPDATED_TOPIC], (event: RelayEvent) => {
    const p = event.payload as Partial<PluginUpdatedPayload> | null;
    onUpdated({ id: typeof p?.id === 'string' ? p.id : '', globalEnabled: p?.globalEnabled === true });
  });
}
