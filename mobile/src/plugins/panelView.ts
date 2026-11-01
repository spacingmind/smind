// panelView.ts: the pure helpers behind a plugin panel screen -- no react, no react-native, so
// vitest's plain node transform can load them. panelErrorView maps a daemon refusal to the words
// the Human sees (DaemonError's code is read duck-typed, a `.code` string, so no relay import is
// needed); eventTouchesPanel decides whether one plugin.event is worth a refetch of this panel;
// panelsOfPlugin picks one plugin's panels out of a plugin.panels result, in order.

import { PluginPanel, PluginPanelsResult } from './types';

/** The error view's words: a title for what went wrong and a body saying what (if anything) to do. */
export function panelErrorView(err: unknown): { title: string; body: string } {
  const message = err instanceof Error ? err.message : String(err);
  const code = err instanceof Error ? (err as unknown as { code?: unknown }).code : undefined;
  if (code === 'view_invalid') {
    // The daemon's message names the JSON path that was too large or invalid; it stays verbatim.
    return { title: 'This panel is too large or invalid', body: message.replace(/^plugin\.view: /, '') };
  }
  if (code === 'ui_level_too_low') return { title: 'This panel needs a newer app', body: 'Update the app to open it.' };
  return { title: "Couldn't load this panel", body: message.replace(/^plugin\.view: /, '') };
}

/** True iff a plugin.event touches this panel: its own plugin and panel, or the plugin as a whole (no panelId). */
export function eventTouchesPanel(p: { pluginId: string; panelId?: string }, pluginId: string, panelId: string): boolean {
  return p.pluginId === pluginId && (p.panelId === undefined || p.panelId === panelId);
}

/** That plugin's panels only, in the daemon's order; panels reachable only via `sidebar` items are not added. */
export function panelsOfPlugin(res: PluginPanelsResult, pluginId: string): PluginPanel[] {
  return res.panels.filter((p) => p.plugin === pluginId);
}
