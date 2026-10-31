import { createContext, useContext } from "react";

/**
 * What a plugin panel's view may ask the app to do (ADR-0042): open another
 * panel (as a task tab, optionally with a context the panel passes through)
 * or run one of the plugin's qualified rpc commands. The panel content is
 * untrusted, so it never gets anything richer than these two verbs.
 */
export interface PluginActions {
  openPanel: (pluginId: string, panelId: string, context?: unknown) => void;
  runCommand: (qualifiedName: string) => boolean;
}

const NOOP_ACTIONS: PluginActions = {
  openPanel: () => {},
  runCommand: () => false,
};

export const PluginActionsContext = createContext<PluginActions>(NOOP_ACTIONS);

/** The panel actions the enclosing app provides; no-ops when nobody provided any (tests, previews). */
export function usePluginPanelActions(): PluginActions {
  return useContext(PluginActionsContext);
}
