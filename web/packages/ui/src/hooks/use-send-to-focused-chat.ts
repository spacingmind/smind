import { useQueryClient } from "@tanstack/react-query";

import { providerListQueryOptions } from "@/hooks/use-provider-catalog";
import { toast } from "@/components/ui/toast";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { recipientRunParams, resolveReviewRecipient } from "@/lib/review-recipient";
import { queryKeys } from "@/lib/query-keys";
import type { Chat, ProviderListResult, RunStartResult } from "@/lib/types";
import { useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * Sends a human prompt to the task's focused chat, the way the Diff pane's "Submit review" does: queued when that
 * chat is mid-run (`whenBusy: "queue"`), under the provider/model/permission settings the chat itself is using.
 * The chats are read at send time (`chat.list`), so a header-level surface needs no chat list of its own. Throws an
 * Error whose message is a sentence when there is nowhere to send.
 */
export function useSendToFocusedChat(taskId: number) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return async (prompt: string): Promise<void> => {
    if (!client) throw new Error("smind isn't connected to the daemon.");
    const chats = (await client.call<Chat[]>("chat.list", { taskId })) ?? [];
    const focusedChatId = useFocusedChatStore.getState().byTask[taskId] ?? null;
    const resolved = resolveReviewRecipient({ taskId, chats, focusedChatId, connected: true, providers: queryClient.getQueryData<ProviderListResult>(queryKeys.providerList())?.providers });
    if (resolved.kind !== "ready") throw new Error("This task has no open chat to send the request to. Open a chat first.");
    const { recipient } = resolved;
    let provider: string | null = recipient.provider;
    if (!provider) {
      provider = "claude-native";
      try {
        const result = await queryClient.fetchQuery(providerListQueryOptions(client));
        if (result?.providers?.[0]) provider = result.providers[0].id;
      } catch {
        // The daemon's own default is the same fallback the composer uses.
      }
    }
    const started = await client.call<RunStartResult>("run.start", {
      taskId,
      chatId: recipient.chatId,
      provider,
      prompt,
      whenBusy: "queue",
      ...recipientRunParams(recipient),
    });
    const chatName = recipient.title || "the chat";
    const queued = "queued" in started;
    toast({
      variant: "success",
      title: queued ? `Request queued in ${chatName}` : `Request sent to ${chatName}`,
      description: queued ? "It sends when the current run finishes." : undefined,
    });
    useFocusedChatStore.getState().requestRevealChat(taskId, recipient.chatId);
  };
}
