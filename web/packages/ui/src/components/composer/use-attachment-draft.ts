import { useCallback, useEffect } from "react";

import type { DraftForgeRef } from "@/stores/composer-attachments-store";
import { useAttachmentLimits } from "@/hooks/use-attachment-limits";
import { attachmentDraftKey, useComposerAttachmentsStore, useDraftAttachments } from "@/stores/composer-attachments-store";

/**
 * The draft attachments of one task chat plus the actions that need the daemon's limits. `limits` is null when the
 * daemon reports no attachment support: every caller then renders nothing and `add` is a no-op.
 */
export function useAttachmentDraft(taskId: number | null, chatId: number | null) {
  const limits = useAttachmentLimits();
  const key = taskId !== null && chatId !== null ? attachmentDraftKey(taskId, chatId) : null;
  const items = useDraftAttachments(key);
  const addToStore = useComposerAttachmentsStore((s) => s.add);
  const addForgeToStore = useComposerAttachmentsStore((s) => s.addForgeRef);
  const removeFromStore = useComposerAttachmentsStore((s) => s.remove);

  const add = useCallback(
    (files: File[]) => {
      if (key && limits && files.length > 0) addToStore(key, files, limits);
    },
    [key, limits, addToStore],
  );
  const addForgeRef = useCallback(
    (ref: Omit<DraftForgeRef, "localId" | "error">) => {
      if (key && limits) addForgeToStore(key, ref, limits);
    },
    [key, limits, addForgeToStore],
  );
  const remove = useCallback((localId: string) => key && removeFromStore(key, localId, limits), [key, limits, removeFromStore]);

  // Drafts are in-memory: leaving the chat (or the page) drops them and frees their object URLs.
  useEffect(() => {
    if (!key) return;
    return () => useComposerAttachmentsStore.getState().clear(key);
  }, [key]);

  return { limits, key, items, add, addForgeRef, remove, hasError: items.some((i) => i.error) };
}
