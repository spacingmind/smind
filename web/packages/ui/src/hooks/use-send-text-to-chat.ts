import { useCallback } from "react";

import { toast } from "@/components/ui/toast";
import { terminalChipLabel, type TerminalBlockLabel } from "@/lib/send-set";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * "Send to agent" text delivery (DT3-D AC14): `sendText` adds one terminal context chip to the focused
 * chat's composer -- the raw text never lands in the draft, its fenced block is built at send time --
 * and shows the same "Added to chat" toast with an "Open chat" action; it never sends and never switches
 * tabs. `canSend` is false without a chat, so the affordance is simply not rendered (AC13).
 */
export function useSendTextToChat(taskId: number): { canSend: boolean; sendText: (body: string, blockLabel: TerminalBlockLabel, description: string) => void } {
  const chatId = useFocusedChatId(taskId);
  const sendText = useCallback(
    (body: string, blockLabel: TerminalBlockLabel, description: string) => {
      if (chatId === null) return;
      useComposerAttachmentsStore.getState().addChip(attachmentDraftKey(taskId, chatId), { kind: "terminal", body, blockLabel, label: terminalChipLabel(body, blockLabel) });
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
