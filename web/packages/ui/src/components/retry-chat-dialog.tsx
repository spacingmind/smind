import { useEffect, useRef, useState } from "react";

import { forkReasonLabel, TRANSIENT_REASONS, type ForkUnavailable } from "@/components/fork-chat-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import type { ForkMode } from "@/hooks/use-fork-chat";
import { RetryStartError } from "@/hooks/use-retry-chat";
import type { Chat } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";

/**
 * The "Edit & retry" dialog: edit the prompt of a turn and run the edit in a
 * new chat that branches at the turn before it -- the original chat is never
 * touched. Like ForkChatDialog, the first attempt is native; a
 * `fork_unavailable` answer with a seeded option switches the same open
 * dialog to the seeded fallback, transient reasons and every other failure
 * stay inline. The caller owns the call (useRetryChat) and what a successful
 * retry opens (onRetried).
 */
export function RetryChatDialog({
  open,
  onOpenChange,
  chatTitle,
  turn,
  initialPrompt,
  hasAnchor,
  onRetry,
  onRetried,
  permissionNote,
  attachmentNames,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The chat whose turn is being replaced, named in the dialog body. */
  chatTitle: string;
  /** The 1-based position of the replaced turn among the chat's runs. */
  turn: number;
  /** The replaced turn's prompt, prefilled into the editor. */
  initialPrompt: string;
  /** Whether an earlier finished turn exists to branch at (retryAnchor !== null). */
  hasAnchor: boolean;
  onRetry: (prompt: string, mode: ForkMode, keepAttachments: boolean) => Promise<Chat>;
  /** What permission mode the retry will run in, in words (it never inherits the replaced turn's mode). */
  permissionNote: string;
  /** The replaced turn's attachments, by name: they are sent again unless the user clears the box. */
  attachmentNames: string[];
  /** Called with the new chat on success (open its tab); also when only the chat could be created, once the user chose to open it. */
  onRetried: (chat: Chat) => void;
}) {
  const action = useConfirmAction(open);
  const [prompt, setPrompt] = useState(initialPrompt);
  const [keepAttachments, setKeepAttachments] = useState(true);
  const [unavailable, setUnavailable] = useState<ForkUnavailable | null>(null);
  // The chat exists but its prompt never started (RetryStartError): the dialog says so and offers the empty chat.
  const [partial, setPartial] = useState<{ chat: Chat; message: string } | null>(null);
  // One retry at a time: two clicks before React re-renders must not start two forks and two runs.
  const inFlight = useRef(false);

  // Every open starts from the replaced turn's own text and the native attempt, so a previous edit never leaks into the next one.
  useEffect(() => {
    if (open) {
      setPrompt(initialPrompt);
      setKeepAttachments(true);
      setUnavailable(null);
      setPartial(null);
    }
  }, [open, initialPrompt]);

  const confirm = async (): Promise<void> => {
    if (partial) {
      onRetried(partial.chat);
      onOpenChange(false);
      return;
    }
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      await action.run(async () => {
        const mode: ForkMode = unavailable ? "seeded" : "native";
        let chat: Chat;
        try {
          chat = await onRetry(prompt, mode, keepAttachments);
        } catch (err) {
          if (mode === "native" && err instanceof RpcError && err.code === "fork_unavailable") {
            const data = (err.data ?? {}) as ForkUnavailable;
            if (data.reason && TRANSIENT_REASONS.has(data.reason)) {
              // Not a limit of the agent: the same retry can work a moment later, so no lossy offer.
              throw new Error(`The session cannot be forked right now: ${forkReasonLabel(data.reason)}.`);
            }
            if (data.seededAvailable) {
              setUnavailable(data); // Not a failure to show -- the dialog's own fallback state.
              return;
            }
          }
          if (err instanceof RetryStartError) {
            setPartial({ chat: err.chat, message: err.message });
            return;
          }
          throw err;
        }
        onRetried(chat);
        onOpenChange(false);
      });
    } finally {
      inFlight.current = false;
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // An action in flight keeps the dialog open: closing it would hide a failure the user is waiting on.
        if (!next && action.pending) return;
        onOpenChange(next);
      }}
    >
      <DialogContent data-testid="dialog-retry-chat">
        <DialogHeader className="text-left">
          <DialogTitle>Edit &amp; retry</DialogTitle>
          <DialogDescription asChild>
            <div>
              From <span className="font-medium text-foreground">"{chatTitle}"</span>, replacing turn {turn}
            </div>
          </DialogDescription>
        </DialogHeader>
        <textarea
          data-testid="retry-prompt"
          autoFocus
          value={prompt}
          rows={5}
          onChange={(e) => setPrompt(e.target.value)}
          className="w-full resize-y rounded-lg border border-input-border bg-input px-2.5 py-1 text-ui-base outline-none hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused"
        />
        <p className="text-ui-sm text-foreground-muted">
          {unavailable
            ? `The agent's own session cannot be branched here (${forkReasonLabel(unavailable.reason)}). The retry can start in a new chat that carries a short account of the earlier turns instead: the agent will not remember tool calls or its reasoning.`
            : hasAnchor
              ? "The edited prompt runs in a new chat that continues from the turn before this one. This chat stays as it is."
              : "There is no earlier turn to branch from, so the edited prompt starts in a new chat. This chat stays as it is."}
        </p>
        {attachmentNames.length > 0 && (
          <label className="flex items-start gap-2 text-ui-sm" data-testid="retry-attachments">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={keepAttachments}
              disabled={action.pending || partial !== null}
              onChange={(e) => setKeepAttachments(e.target.checked)}
              data-testid="retry-attachments-keep"
            />
            <span>
              Send the {attachmentNames.length === 1 ? "attachment" : `${attachmentNames.length} attachments`} of this turn again
              <span className="block text-foreground-muted">{attachmentNames.join(", ")}</span>
            </span>
          </label>
        )}
        <p className="text-ui-sm text-foreground-muted" data-testid="retry-permission-note">
          {permissionNote}
        </p>
        <FormError message={partial?.message ?? action.error} testId="dialog-retry-chat-error" />
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={action.pending}
            data-testid="dialog-retry-chat-cancel"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={action.pending || (!partial && prompt.trim() === "")}
            data-testid="dialog-retry-chat-confirm"
            onClick={() => void confirm()}
          >
            {action.pending ? "Retrying…" : partial ? "Open the new chat" : unavailable ? "Retry with a summary" : "Retry"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
