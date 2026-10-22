import { useCallback } from "react";

import { useAttachmentLimits } from "@/hooks/use-attachment-limits";
import { supportsKind } from "@/lib/attachments/kinds";
import type { MentionRef } from "@/lib/file-mention";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * "Add to chat" for the Diff and Files panes: `add(ref)` queues the `@path[#Lx-Ly]` token for the focused chat's
 * composer (which appends it and remembers the ref) and asks App to bring that chat forward. `canAdd` is false
 * without a chat or when the daemon does not accept `file_ref`, so the affordance is simply not rendered.
 */
export function useAddToChat(taskId: number): { canAdd: boolean; add: (ref: MentionRef) => void } {
  const chatId = useFocusedChatId(taskId);
  const limits = useAttachmentLimits();
  const canAdd = chatId !== null && supportsKind(limits, "file_ref");
  const add = useCallback(
    (ref: MentionRef) => {
      if (chatId === null) return;
      useComposerAttachmentsStore.getState().queueInsert(attachmentDraftKey(taskId, chatId), ref);
      useFocusedChatStore.getState().requestRevealChat(taskId, chatId);
    },
    [taskId, chatId],
  );
  return { canAdd, add };
}
