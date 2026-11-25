import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { BrowserSettingsView } from "@/lib/browser-settings-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { WsClient } from "@/lib/ws-client";

/**
 * The agent browser's per-workspace settings (ADR-0061) on the shared query cache (ADR-0032). All three RPCs
 * are human-only and local-only: `browser.settings.get` / `.set` and `browser.data.clear`. The view is the
 * only thing the Settings -> Browser section reads, and a mutation writes the view the daemon answers with
 * straight into the cache. There is no daemon event for a browser session starting or ending, so the view is
 * re-read every `BROWSER_SETTINGS_POLL_MS` while a section is mounted (the live indicator follows a run).
 */

/** How often the mounted section re-reads the view (a run's browser session starts and ends without an event). */
export const BROWSER_SETTINGS_POLL_MS = 3000;

function requireClient(client: WsClient | null): WsClient {
  if (!client) throw new Error("no daemon connection");
  return client;
}

/** The workspace's browser settings view; `null` workspaceId = nothing to read. Normalises a null list to []. */
export function useBrowserSettings(workspaceId: number | null) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.browserSettings(workspaceId ?? 0),
    enabled: client !== null && workspaceId !== null,
    retry: false,
    refetchInterval: BROWSER_SETTINGS_POLL_MS,
    queryFn: async (): Promise<BrowserSettingsView> => {
      const view = await requireClient(client).call<BrowserSettingsView>("browser.settings.get", { workspaceId });
      return { ...view, allowHosts: view.allowHosts ?? [], providers: view.providers ?? [] };
    },
  });
}

/** The change one `browser.settings.set` call may carry: exactly the fields the daemon accepts, nothing else. */
export type BrowserSettingsChange =
  | { enabled: boolean }
  | { allowAdd: [string] }
  | { allowRemove: [string] };

/**
 * `browser.settings.set` for one workspace. The daemon's refusal text (a wildcard, a scheme, a full list...)
 * is the mutation's error message, as-is; the returned view replaces the cached one.
 */
export function useSetBrowserSettings(workspaceId: number | null) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation({
    // A poll already in flight would resolve AFTER the answer below and put the older view back: cancel it first.
    onMutate: async () => {
      if (workspaceId !== null) await queryClient.cancelQueries({ queryKey: queryKeys.browserSettings(workspaceId) });
    },
    mutationFn: async (change: BrowserSettingsChange): Promise<BrowserSettingsView> => {
      if (workspaceId === null) throw new Error("no workspace");
      const view = await requireClient(client).call<BrowserSettingsView>("browser.settings.set", { workspaceId, ...change });
      return { ...view, allowHosts: view.allowHosts ?? [], providers: view.providers ?? [] };
    },
    onSuccess: (view) => {
      if (workspaceId !== null) queryClient.setQueryData(queryKeys.browserSettings(workspaceId), view);
    },
  });
}

/** `browser.data.clear`: ends the workspace's browser session and removes its (ephemeral) profile; re-reads the view. */
export function useClearBrowserData(workspaceId: number | null) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation({
    onMutate: async () => {
      if (workspaceId !== null) await queryClient.cancelQueries({ queryKey: queryKeys.browserSettings(workspaceId) });
    },
    mutationFn: async (): Promise<void> => {
      if (workspaceId === null) throw new Error("no workspace");
      await requireClient(client).call("browser.data.clear", { workspaceId });
    },
    onSuccess: () => {
      if (workspaceId !== null) void queryClient.invalidateQueries({ queryKey: queryKeys.browserSettings(workspaceId) });
    },
  });
}
