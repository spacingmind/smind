/**
 * The declarative panels of the ACTIVE plugins (ADR-0042): the wire shapes
 * of the daemon's `plugin.panels` result. Panels are data, not JSX -- the
 * daemon withholds any panel whose `ui` level is above what this client
 * states (see `PLUGIN_UI_LEVEL`), so a client never hears about a panel it
 * cannot render.
 */

/** The view schema level this client understands (ADR-0042 section 5): sent with every `plugin.panels` / `plugin.view` call. */
export const PLUGIN_UI_LEVEL = 1;

/** One panel an active plugin contributes. `icon?` is a name in `pluginIconByName` (lib/icons.ts). */
export interface PluginPanel {
  plugin: string;
  id: string;
  title: string;
  icon?: string;
  /** Where the panel is surfaced; the task tab strip serves "task" and "palette", the sidebar "sidebar". */
  where: "task" | "sidebar" | "palette";
  /** The panel's view schema level; the daemon only sends panels this client's `PLUGIN_UI_LEVEL` covers. */
  ui: number;
}

/** One sidebar entry an active plugin contributes, opening one of its panels. */
export interface PluginSidebarItem {
  plugin: string;
  id: string;
  title: string;
  icon?: string;
  /** The plugin-local panel id this entry opens. */
  opens: string;
}

/** The `plugin.panels` result. */
export interface PluginPanelsResult {
  panels: PluginPanel[];
  sidebar: PluginSidebarItem[];
}
