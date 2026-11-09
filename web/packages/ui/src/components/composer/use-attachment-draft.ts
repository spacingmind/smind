import { useCallback, useEffect } from "react";

import type { DraftForgeRef } from "@/stores/composer-attachments-store";
import { useAttachmentLimits } from "@/hooks/use-attachment-limits";
import { attachmentDraftKey, HOME_ATTACHMENT_KEY, useComposerAttachmentsStore, useDraftAttachments } from "@/stores/composer-attachments-store";

/** The tray actions of one store key; a null key makes them no-ops. */
function useAttachmentTray(key: string | null) {
  const limits = useAttachmentLimits();
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

  return { limits, key, items, add, addForgeRef, remove, hasError: items.some((i) => i.error) };
}

/**
 * The draft attachments of one task chat plus the actions that need the daemon's limits. `limits` is null when the
 * daemon reports no attachment support: every caller then renders nothing and `add` is a no-op.
 */
export function useAttachmentDraft(taskId: number | null, chatId: number | null) {
  const key = taskId !== null && chatId !== null ? attachmentDraftKey(taskId, chatId) : null;
  const tray = useAttachmentTray(key);

  // Drafts are in-memory: leaving the chat (or the page) drops them and frees their object URLs.
  useEffect(() => {
    if (!key) return;
    return () => useComposerAttachmentsStore.getState().clear(key);
  }, [key]);

  return tray;
}

/**
 * The home landing's tray (no task yet). On unmount it clears only what is still there: a successful move to the new
 * task's key leaves it empty, and the moved items must keep their object URLs.
 */
export function useHomeAttachmentDraft() {
  const tray = useAttachmentTray(HOME_ATTACHMENT_KEY);
  useEffect(() => {
    return () => {
      if ((useComposerAttachmentsStore.getState().byKey[HOME_ATTACHMENT_KEY] ?? []).length > 0) useComposerAttachmentsStore.getState().clear(HOME_ATTACHMENT_KEY);
    };
  }, []);
  return tray;
}
