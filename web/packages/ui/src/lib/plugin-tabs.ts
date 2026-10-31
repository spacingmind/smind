import type { TabEntry } from "@/components/tab-registry";
import type { PluginPanel } from "@/lib/plugin-panel-types";

/**
 * The plugin-kind tabs whose (pluginId, panelId) pair the daemon's panel
 * list no longer has (ADR-0042): what the shell closes when a plugin was
 * disabled, removed or reloaded without that panel. A tab carries no
 * matching pair at all (a hand-edited persisted blob) counts as absent --
 * there is nothing it could render.
 */
export function pluginTabsToClose(
  tabs: readonly TabEntry[],
  panels: readonly Pick<PluginPanel, "plugin" | "id">[],
): TabEntry[] {
  const live = new Set(panels.map((p) => `${p.plugin}\u0000${p.id}`));
  return tabs.filter((t) => t.kind === "plugin" && !live.has(`${t.pluginId ?? ""}\u0000${t.panelId ?? ""}`));
}
