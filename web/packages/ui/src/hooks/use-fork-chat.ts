import { useCallback } from "react";

import type { Chat } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** How the daemon makes the fork (chat.fork's `mode`): the agent's own session branched, or a summary seed. */
export type ForkMode = "native" | "seeded";

/**
 * `chat.fork` behind the "Fork from here" action: no caching -- the new
 * chat arrives both as the call's own return and as the chat.created event
 * useTaskChats already applies to the task's chat list.
 */
export function useForkChat(client: WsClientLike | null, taskId: number) {
  const fork = useCallback(
    (chatId: number, runId: string, mode: ForkMode): Promise<Chat> => {
      if (!client) return Promise.reject(new Error(`no daemon connection (task ${taskId})`));
      return client.call<Chat>("chat.fork", { chatId, atRunId: runId, mode });
    },
    [client, taskId],
  );
  return { fork };
}
