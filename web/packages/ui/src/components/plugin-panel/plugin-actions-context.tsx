import { createContext, useContext } from "react";

/**
 * What a plugin panel's view may ask the app to do (ADR-0042): open another
 * panel, optionally with a context the panel passes through. Running one of
 * the plugin's qualified rpc commands is NOT a panel verb -- the executor
 * handles those itself, so the untrusted panel content never gets a
 * "run anything" escape hatch.
 */
export interface PluginActions {
  openPanel: (pluginId: string, panelId: string, context?: unknown) => void;
}

const NOOP_ACTIONS: PluginActions = {
  openPanel: () => {},
};

export const PluginActionsContext = createContext<PluginActions>(NOOP_ACTIONS);

/** What App.tsx provides around the app content; the plain Provider, so tests can seed actions too. */
export const PluginActionsProvider = PluginActionsContext.Provider;

/** The panel actions the enclosing app provides; no-ops when nobody provided any (tests, previews). */
export function usePluginPanelActions(): PluginActions {
  return useContext(PluginActionsContext);
}
