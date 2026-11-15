import { useCallback } from "react";

import { useAttachmentLimits } from "@/hooks/use-attachment-limits";
import { supportsKind } from "@/lib/attachments/kinds";
import { toast } from "@/components/ui/toast";
import { formatMentionToken, type MentionRef } from "@/lib/file-mention";
import { fileRefChipLabel } from "@/lib/send-set";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * "Send to agent" reference delivery for the file editor (DT3-D AC16): `sendRef` adds one file_ref
 * context chip to the focused chat's composer -- no `@path` token goes into the draft; the reference
 * leaves as a `file_ref` attachment at send time, deduped against any such token already there -- and
 * shows the same "Added to chat" toast as "Add to chat". `canSend` is false without a chat or when the
 * daemon does not accept `file_ref`, so the affordance is simply not rendered.
 */
export function useSendRefToChat(taskId: number): { canSend: boolean; sendRef: (ref: MentionRef, bytes: number) => void } {
  const chatId = useFocusedChatId(taskId);
  const limits = useAttachmentLimits();
  const canSend = chatId !== null && supportsKind(limits, "file_ref");
  const sendRef = useCallback(
    (ref: MentionRef, bytes: number) => {
      if (chatId === null) return;
      useComposerAttachmentsStore.getState().addChip(attachmentDraftKey(taskId, chatId), { kind: "file_ref", ref, label: fileRefChipLabel(ref, bytes) });
      toast({
        variant: "success",
        title: "Added to chat",
        description: formatMentionToken(ref),
        action: { label: "Open chat", onClick: () => useFocusedChatStore.getState().requestRevealChat(taskId, chatId) },
      });
    },
    [taskId, chatId],
  );
  return { canSend, sendRef };
}
