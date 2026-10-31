import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import { PLUGIN_UI_LEVEL, type PluginPanel, type PluginPanelsResult } from "@/lib/plugin-panel-types";

/**
 * The declarative panels of the ACTIVE plugins (ADR-0042), one cache entry per
 * connection (ADR-0032), refetched by the event bridge on `plugin.updated`. A
 * daemon without the RPC answers with an error, which reads as "no panels"
 * here rather than as a failure the UI shows -- same discipline as
 * `use-plugin-contributions.ts`.
 */
const NONE_PANELS: PluginPanel[] = [];
const NONE_SIDEBAR: PluginPanelsResult["sidebar"] = [];
const NONE_RESULT: PluginPanelsResult = { panels: NONE_PANELS, sidebar: NONE_SIDEBAR };

export function usePluginPanelsQuery() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.pluginPanels(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<PluginPanelsResult> => {
      if (!client) throw new Error("no daemon connection");
      return await client.call<PluginPanelsResult>("plugin.panels", { ui: PLUGIN_UI_LEVEL });
    },
  });
}

/** The cached panels and sidebar items, one stable empty pair until they load or if the daemon has none. */
export function usePluginPanels(): PluginPanelsResult {
  const result = usePluginPanelsQuery().data;
  return useMemo(
    () =>
      result
        ? { panels: result.panels ?? NONE_PANELS, sidebar: result.sidebar ?? NONE_SIDEBAR }
        : NONE_RESULT,
    [result],
  );
}

/**
 * One panel's validated view document (`plugin.view`): opaque to the data
 * layer -- typed `unknown`, the renderer owns its interpretation. Typed
 * failures (a disabled plugin, an unknown panel, an invalid document)
 * arrive as rpc errors, so they land on `error`, never on `data`.
 */
export function usePluginViewQuery({
  pluginId,
  panelId,
  context,
  enabled,
}: {
  pluginId: string;
  panelId: string;
  context?: unknown;
  enabled?: boolean;
}) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.pluginView(pluginId, panelId, context),
    enabled: enabled !== false && client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<unknown> => {
      if (!client) throw new Error("no daemon connection");
      const result = await client.call<{ view?: unknown }>("plugin.view", {
        pluginId,
        panelId,
        ui: PLUGIN_UI_LEVEL,
        context,
      });
      return result?.view;
    },
  });
}

/** The panel (plugin, panelId) names, or undefined when the daemon's list does not have it. */
export function findPanel(
  panels: readonly PluginPanel[],
  pluginId: string,
  panelId: string,
): PluginPanel | undefined {
  return panels.find((p) => p.plugin === pluginId && p.id === panelId);
}
