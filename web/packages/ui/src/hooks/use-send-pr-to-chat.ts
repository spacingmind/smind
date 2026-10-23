import { useCallback } from "react";

import { describePrError } from "@/components/git/pr-error";
import { toast } from "@/components/ui/toast";
import { useAttachmentLimits } from "@/hooks/use-attachment-limits";
import { usePRCheckLog } from "@/hooks/use-pr-details";
import { supportsKind } from "@/lib/attachments/kinds";
import { allUnresolvedToChat, checkToChat, threadToChat, type PRFileRef } from "@/lib/pr-chat-context";
import type { CheckItem, PRDetails, ReviewThread } from "@/lib/types";
import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { useFocusedChatId, useFocusedChatStore } from "@/stores/focused-chat-store";

/**
 * "Send to chat" for the PR panel: puts the quoted thread/check text into the focused chat's composer as a normal
 * human prompt, with a `pr` chip and `file_ref` tokens when the daemon accepts those kinds. It never sends and never
 * switches tabs; the toast offers "Open chat". `canSend` is false without a focused chat, so the affordance is simply
 * not rendered, and the senders are then no-ops.
 */
export function useSendPRToChat(taskId: number) {
  const chatId = useFocusedChatId(taskId);
  const limits = useAttachmentLimits();
  const checkLog = usePRCheckLog();

  const deliver = useCallback(
    (details: PRDetails, text: string, fileRefs: PRFileRef[], description: string) => {
      if (chatId === null) return;
      const key = attachmentDraftKey(taskId, chatId);
      const store = useComposerAttachmentsStore.getState();
      if (limits && supportsKind(limits, "pr")) {
        store.addForgeRef(
          key,
          { kind: "pr", forge: "github", number: details.number, repo: details.repo, title: details.title, url: details.url },
          limits,
        );
      }
      if (supportsKind(limits, "file_ref")) {
        for (const ref of fileRefs) {
          const mention = ref.line ? { path: ref.path, line: ref.line } : { path: ref.path };
          store.queueInsert(key, mention);
        }
      }
      store.queueText(key, text);
      toast({
        variant: "success",
        title: "Added to chat",
        description,
        action: { label: "Open chat", onClick: () => useFocusedChatStore.getState().requestRevealChat(taskId, chatId) },
      });
    },
    [taskId, chatId, limits],
  );

  const sendThread = useCallback(
    (details: PRDetails, thread: ReviewThread) => {
      const { text, fileRef } = threadToChat(details, thread);
      deliver(details, text, fileRef ? [fileRef] : [], "Review thread");
    },
    [deliver],
  );

  const sendAllUnresolved = useCallback(
    (details: PRDetails) => {
      const { text, fileRefs } = allUnresolvedToChat(details);
      deliver(details, text, fileRefs, "Unresolved review threads");
    },
    [deliver],
  );

  /** Reads the failing check's log tail now (never earlier), then queues it. A failed read sends nothing and says why. */
  const sendCheck = useCallback(
    async (details: PRDetails, check: CheckItem) => {
      if (chatId === null) return;
      let tail = null;
      if (check.state === "failure") {
        try {
          tail = await checkLog.mutateAsync({ taskId, checkId: check.id });
        } catch (err) {
          toast({ variant: "error", title: describePrError(err, "checklog").message });
          return;
        }
      }
      deliver(details, checkToChat(details, check, tail).text, [], "Check");
    },
    [chatId, taskId, checkLog, deliver],
  );

  return { canSend: chatId !== null, sendThread, sendAllUnresolved, sendCheck, checkLogPending: checkLog.isPending };
}
