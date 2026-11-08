import { useEffect, useState } from "react";

import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import type { ForkMode } from "@/hooks/use-fork-chat";
import type { Chat } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";

/** chat.fork's refusal detail (rpcError.data): why native is impossible and whether a seeded fork is offered. */
interface ForkUnavailable {
  reason?: string;
  seededAvailable?: boolean;
}

/** Refusals that pass: the chat is still running or the daemon is busy. */
const TRANSIENT_REASONS = new Set(["native_session_in_use", "rebuild_busy"]);

/** The daemon's refusal reasons (internal/history/native Reason values) as words. */
export function forkReasonLabel(reason: string | undefined): string {
  switch (reason) {
    case "fork_unsupported":
      return "this agent has no session fork";
    case "native_session_missing":
      return "its session file is gone";
    case "native_session_in_use":
      return "the chat is still running";
    case "slice_not_found":
      return "this turn cannot be told apart in the session";
    case "format_drift":
      return "the session format was not recognised";
    case "provider_cannot_rebuild":
      return "no session was recorded for this turn";
    case "rebuild_busy":
      return "the daemon is busy, try again";
    default:
      return "unavailable";
  }
}

/**
 * The "Fork from here" confirm: first the native fork (the agent branches
 * its own session, memory intact); when the daemon answers
 * `fork_unavailable` with `seededAvailable`, the same open dialog switches
 * to offering the seeded fork (a summary seed, lossy) instead. Any other
 * refusal stays inline (useConfirmAction's error), like every other
 * confirm dialog. The caller owns the call (useForkChat) and what a
 * successful fork opens (onForked).
 */
export function ForkChatDialog({
  open,
  onOpenChange,
  chatTitle,
  turn,
  runId,
  onFork,
  onForked,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The chat being forked, named in the dialog body like the archive confirm does. */
  chatTitle: string;
  /** The 1-based turn being forked; when given the title line names it. */
  turn?: number;
  /** The turn to fork at -- also what resets the fallback offer when the target changes. */
  runId: string;
  onFork: (mode: ForkMode) => Promise<Chat>;
  /** Called with the new chat on success (open its tab); optional so a bare pane can still fork. */
  onForked?: (chat: Chat) => void;
}) {
  const action = useConfirmAction(open);
  const [unavailable, setUnavailable] = useState<ForkUnavailable | null>(null);

  // Every close, and every change of target turn while open, restarts from the native attempt.
  useEffect(() => {
    if (!open) setUnavailable(null);
  }, [open, runId]);

  const attempt = async (mode: ForkMode): Promise<Chat | null> => {
    try {
      return await onFork(mode);
    } catch (err) {
      if (mode === "native" && err instanceof RpcError && err.code === "fork_unavailable") {
        const data = (err.data ?? {}) as ForkUnavailable;
        if (data.reason && TRANSIENT_REASONS.has(data.reason)) {
          // Not a limit of the agent: the same fork can work a moment later, so no lossy offer.
          throw new Error(`The session cannot be forked right now: ${forkReasonLabel(data.reason)}.`);
        }
        if (data.seededAvailable) {
          setUnavailable(data);
          return null; // Not a failure to show -- the dialog's own fallback state.
        }
      }
      throw err;
    }
  };

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Fork from here"
      description={
        <>
          <span className="font-medium text-foreground">{turn ? `From "${chatTitle}", turn ${turn}` : chatTitle}</span>
          <p className="mt-2">
            {unavailable
              ? `The agent's own session cannot be branched here (${forkReasonLabel(unavailable.reason)}). You can start a new chat that carries a short account of the earlier turns instead: the agent will not remember tool calls or its reasoning.`
              : "Continue in a new chat from this turn. The agent keeps its memory of the conversation up to here. Files are not changed."}
          </p>
        </>
      }
      confirmLabel={unavailable ? "Fork with a summary" : "Fork"}
      pendingLabel="Forking…"
      pending={action.pending}
      error={action.error}
      destructive={false}
      testId="dialog-fork-chat"
      onConfirm={async () => {
        await action.run(async () => {
          const chat = await attempt(unavailable ? "seeded" : "native");
          if (chat) {
            onForked?.(chat);
            onOpenChange(false);
          }
        });
      }}
    />
  );
}
