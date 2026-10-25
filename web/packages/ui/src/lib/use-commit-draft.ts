import { useCallback, useEffect, useRef, useState } from "react";

import type { TaskDraftCommitMessageResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

export type CommitDraftStatus = "idle" | "drafting" | "error";
export interface CommitDraftError {
  code: string | null;
  message: string;
}

/** Calm copy for the typed task.draftCommitMessage failures; anything else shows the server message. */
export function describeDraftError(error: CommitDraftError): string {
  switch (error.code) {
    case "nothing_staged":
      return "Nothing is staged.";
    case "no_draft_provider":
      return "No Claude account is available to draft a message.";
    case "rate_limited":
      return "Drafting too often — try again in a moment.";
    case "busy":
      return "Another draft is running — try again shortly.";
    case "empty_draft":
      return "The model returned nothing — try again.";
    default:
      return error.message;
  }
}

/**
 * One AI commit-message draft at a time for a task (ADR-0043). `draft()` resolves with the result, or null when
 * it failed, was ignored (already drafting) or went stale (task changed / unmounted); a stale result never
 * touches state, so it can't fill another task's textarea.
 */
export function useCommitDraft(client: WsClientLike | null, taskId: number) {
  const [status, setStatus] = useState<CommitDraftStatus>("idle");
  const [error, setError] = useState<CommitDraftError | null>(null);
  // Bumped on cancel/task change/unmount; a call only applies its result if its generation is still current.
  const generation = useRef(0);
  const inFlight = useRef(false);

  const cancel = useCallback(() => {
    generation.current += 1;
    inFlight.current = false;
    setStatus("idle");
    setError(null);
  }, []);

  useEffect(() => {
    // A new task starts clean: a draft still in flight for the previous one must not leave "drafting" behind.
    setStatus("idle");
    setError(null);
    return () => {
      generation.current += 1;
      inFlight.current = false;
    };
  }, [taskId]);

  const draft = useCallback(async (): Promise<TaskDraftCommitMessageResult | null> => {
    if (!client || inFlight.current) return null;
    const mine = ++generation.current;
    inFlight.current = true;
    setStatus("drafting");
    setError(null);
    try {
      const result = await client.call<TaskDraftCommitMessageResult>("task.draftCommitMessage", { taskId });
      if (generation.current !== mine) return null;
      inFlight.current = false;
      setStatus("idle");
      return result;
    } catch (err) {
      if (generation.current !== mine) return null;
      inFlight.current = false;
      const code = (err as { code?: unknown } | null)?.code;
      setError({ code: typeof code === "string" ? code : null, message: err instanceof Error ? err.message : String(err) });
      setStatus("error");
      return null;
    }
  }, [client, taskId]);

  return { status, error, draft, cancel };
}
