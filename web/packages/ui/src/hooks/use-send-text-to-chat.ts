import { useCallback } from "react";

import { toast } from "@/components/ui/toast";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * "Send to agent" text delivery (DT3-C AC11): `sendText` queues one text block into the focused
 * chat's composer via `queueText` -- the same pending-draft path "Add to chat" uses -- and shows the
 * same "Added to chat" toast with an "Open chat" action; it never sends and never switches tabs.
 * `canSend` is false without a chat, so the affordance is simply not rendered (AC13).
 */
export function useSendTextToChat(taskId: number): { canSend: boolean; sendText: (text: string, description: string) => void } {
  const chatId = useFocusedChatId(taskId);
  const sendText = useCallback(
    (text: string, description: string) => {
      if (chatId === null) return;
      useComposerAttachmentsStore.getState().queueText(attachmentDraftKey(taskId, chatId), text);
      toast({
        variant: "success",
        title: "Added to chat",
        description,
        action: { label: "Open chat", onClick: () => useFocusedChatStore.getState().requestRevealChat(taskId, chatId) },
      });
    },
    [taskId, chatId],
  );
  return { canSend: chatId !== null, sendText };
}
