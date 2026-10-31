import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { usePluginPanelActions } from "@/components/plugin-panel/plugin-actions-context";
import { toast } from "@/components/ui/toast";
import { usePluginCommands } from "@/hooks/use-plugin-contributions";
import { openExternalUrl } from "@/lib/external-link";
import { queryKeys } from "@/lib/query-keys";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { slashRpcPrompt } from "@/lib/slash-commands";
import type { PluginActionExtra, PluginViewAction } from "@/lib/plugin-view-types";
import { useComposerInsertStore } from "@/stores/composer-insert-store";

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === "object" && x !== null && !Array.isArray(x);
}

/**
 * Runs the actions a plugin panel's view asks for (ADR-0042). The document
 * is untrusted, so every action is validated before use and a `kind` that
 * is not one of the five is ignored, never thrown. Text only ever reaches
 * the composer's draft (via the insert store) -- nothing here can send a
 * run -- and a url is only opened after the caller confirms it.
 */
export function usePluginActionRunner({ pluginId, panelId }: { pluginId: string; panelId: string }) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const commands = usePluginCommands();
  const { openPanel } = usePluginPanelActions();
  const [pendingUrl, setPendingUrl] = useState<{ url: string; host: string } | null>(null);

  /** Text goes to the composer that last had focus; with none mounted there is nothing to insert into. */
  const insertIntoComposer = useCallback((text: string) => {
    if (!useComposerInsertStore.getState().insert(text)) {
      toast({ title: "Open a chat to insert this text" });
    }
  }, []);

  /** One plugin rpc; a failure is toasted here and reads as `{ ok: false }` to the caller. */
  const callRpc = useCallback(
    async (
      plugin: string,
      rpc: string,
      params: Record<string, unknown>,
    ): Promise<{ ok: true; result: unknown } | { ok: false }> => {
      if (!client) return { ok: false };
      try {
        const result = await client.call<unknown>("plugin.call", { pluginId: plugin, rpc, params });
        return { ok: true, result };
      } catch (err) {
        toast({
          variant: "error",
          title: "Plugin action failed",
          description: err instanceof Error ? err.message : String(err),
        });
        return { ok: false };
      }
    },
    [client],
  );

  const run = useCallback(
    async (action: PluginViewAction, extra?: PluginActionExtra) => {
      if (typeof action !== "object" || action === null) return;
      switch (action.kind) {
        case "rpc": {
          if (typeof action.rpc !== "string" || action.rpc === "") return;
          const params: Record<string, unknown> = isRecord(action.params) ? { ...action.params } : {};
          const formValues = extra?.formValues;
          if (isRecord(formValues)) params.values = formValues;
          const outcome = await callRpc(pluginId, action.rpc, params);
          if (outcome.ok) {
            // The panel's document is the rpc's visible effect: refresh it.
            await queryClient.invalidateQueries({ queryKey: queryKeys.pluginViewOfPanel(pluginId, panelId) });
          }
          return;
        }
        case "prompt":
          if (typeof action.text === "string" && action.text !== "") insertIntoComposer(action.text);
          return;
        case "command": {
          if (typeof action.name !== "string" || action.name === "") return;
          const cmd = commands.find((c) => c.qualified === action.name);
          if (!cmd) {
            toast({ title: `Command not found: ${action.name}` });
            return;
          }
          if (typeof cmd.prompt === "string" && cmd.prompt !== "") {
            insertIntoComposer(cmd.prompt);
            return;
          }
          if (typeof cmd.rpc === "string" && cmd.rpc !== "") {
            const outcome = await callRpc(cmd.plugin, cmd.rpc, { command: cmd.name, args: "" });
            if (!outcome.ok) return;
            const prompt = slashRpcPrompt(outcome.result);
            if (prompt === null) {
              toast({ variant: "error", title: `Plugin ${cmd.plugin} did not return prompt text.` });
              return;
            }
            insertIntoComposer(prompt);
          }
          return;
        }
        case "openPanel":
          if (typeof action.panel !== "string" || action.panel === "") return;
          openPanel(pluginId, action.panel, action.context);
          return;
        case "openUrl": {
          // Never opened directly: the caller confirms the host first. The
          // document's `host` field is untrusted -- the host shown (and
          // confirmed) is derived from the url itself, and only a clean
          // http(s) url with no embedded credentials may ask at all.
          if (typeof action.url !== "string" || action.url === "") return;
          let parsed: URL;
          try {
            parsed = new URL(action.url);
          } catch {
            return;
          }
          if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || parsed.username !== "" || parsed.password !== "") {
            return;
          }
          setPendingUrl({ url: action.url, host: parsed.host });
          return;
        }
        default:
          return;
      }
    },
    [callRpc, commands, insertIntoComposer, openPanel, panelId, pluginId, queryClient],
  );

  const confirmUrl = useCallback(() => {
    if (!pendingUrl) return;
    openExternalUrl(pendingUrl.url);
    setPendingUrl(null);
  }, [pendingUrl]);

  const cancelUrl = useCallback(() => setPendingUrl(null), []);

  return { run, pendingUrl, confirmUrl, cancelUrl };
}
