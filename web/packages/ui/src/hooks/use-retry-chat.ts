import { useCallback } from "react";

import type { ForkMode } from "@/hooks/use-fork-chat";
import type { RunEntry } from "@/hooks/use-run-timeline";
import { resendRequests } from "@/lib/attachments/reuse";
import { runStartParams } from "@/lib/run-start";
import type { Chat, PermissionSettings } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * The turn an "Edit & retry" branches at: the last finished, real (not
 * child-event) run before the replaced one. A run that errored or was
 * stopped is not a place the agent's history can continue from, so the
 * search keeps walking back; null means there is nothing to branch from
 * and the edited prompt starts a plain new chat instead.
 */
export function retryAnchor(runs: RunEntry[], run: RunEntry): RunEntry | null {
  for (let i = runs.indexOf(run) - 1; i >= 0; i--) {
    const candidate = runs[i];
    if (candidate.status === "done" && candidate.origin !== "child_event") return candidate;
  }
  return null;
}

/**
 * "Edit & retry" behind the prompt action bar: fork the chat at the turn
 * before the replaced one (kind "retry") and start the edited prompt in
 * the child -- the original chat is never touched. With no anchor there
 * is nothing to branch from, so the prompt starts in a fresh chat.create.
 */
export function useRetryChat(client: WsClientLike | null) {
  const retry = useCallback(
    (args: {
      taskId: number;
      chat: Chat;
      runs: RunEntry[];
      /** The turn being replaced. */
      run: RunEntry;
      prompt: string;
      mode: ForkMode;
      permission: PermissionSettings;
    }): Promise<Chat> => {
      const { taskId, chat, runs, run, prompt, mode, permission } = args;
      if (!client) return Promise.reject(new Error("no daemon connection"));
      const anchor = retryAnchor(runs, run);
      const child = anchor
        ? client.call<Chat>("chat.fork", { chatId: chat.ID, atRunId: anchor.id, mode, kind: "retry" })
        : client.call<Chat>("chat.create", { taskId });
      // ADR-0038: the earlier run's attachments ride along as `reuse`/ref requests (the daemon copies the stored files).
      const reuse = resendRequests(run.id, run.attachments);
      return child.then(async (forked) => {
        try {
          await client.call(
            "run.start",
            runStartParams({
              taskId,
              chatId: forked.ID,
              provider: run.provider,
              prompt,
              whenBusy: "queue",
              permission,
              thinkingLevel: run.thinkingLevel,
              model: run.model || undefined,
              attachments: reuse.length > 0 ? reuse : undefined,
            }),
          );
        } catch (err) {
          // The chat exists even though nothing runs in it: hand it back so the caller can still open it.
          const failure = new Error(
            `The new chat was created but the prompt could not be started: ${err instanceof Error ? err.message : String(err)}`,
          );
          (failure as { chat?: Chat }).chat = forked;
          throw failure;
        }
        return forked;
      });
    },
    [client],
  );
  return { retry };
}
