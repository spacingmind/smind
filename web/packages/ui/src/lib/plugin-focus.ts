/**
 * One-slot handoff from an owner badge ("from plugin X") to Settings -> Plugins: the badge records which plugin it
 * names and opens the section; the section takes it on mount (Settings remounts onto the requested section) or via
 * subscribe when it is already showing, and expands that plugin's detail. Same shape as lib/settings-intent.ts.
 */
let pending: string | null = null;
const listeners = new Set<() => void>();

export const PLUGINS_SECTION_ID = "plugins";

export function setPluginFocus(id: string): void {
  pending = id;
  for (const listener of [...listeners]) listener();
}

/** Returns the pending plugin id and clears it. */
export function takePluginFocus(): string | null {
  const id = pending;
  pending = null;
  return id;
}

export function subscribePluginFocus(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Opens Settings -> Plugins on that plugin's detail. */
export function openPluginDetail(id: string): void {
  setPluginFocus(id);
  window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: PLUGINS_SECTION_ID } }));
}
