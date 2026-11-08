import { useEffect, useState } from "react";

import { useRunTimeline, type RunEntry } from "@/hooks/use-run-timeline";
import type { Chat, Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * The parent's runs up to and including runId -- the turns a native fork's
 * agent still remembers, and the numbered turn the banner names. Empty when
 * runId is not among them (a parent whose history no longer reaches the
 * fork point). Pure so tests can check the slicing without a mount.
 */
export function inheritedUpTo(runs: RunEntry[], runId: string): RunEntry[] {
  const index = runs.findIndex((run) => run.id === runId);
  return index === -1 ? [] : runs.slice(0, index + 1);
}

/**
 * Where a forked chat's earlier turns come from (ADR-0057): the parent
 * chat's own runs for a native fork (the agent session branched, so those
 * turns are still real), or the summary seed the daemon prepended for a
 * seeded fork (chat.forkSeed). `turn` is the fork point's 1-based position
 * in the parent's runs -- the number the banner and the fork dialog name.
 *
 * A non-fork loads nothing: useRunTimeline is still called (hooks must be
 * unconditional) but with null ids, which leaves it idle and issues no
 * calls.
 */
export function useForkSource(client: WsClientLike | null, task: Task, chat: Chat) {
  const fork = chat.fork ?? null;
  const { runs } = useRunTimeline(client, fork ? task.ID : null, fork?.parentChatId ?? null, null);
  const [seed, setSeed] = useState<string | null>(null);
  const seeded = fork?.mode === "seeded";

  useEffect(() => {
    setSeed(null);
    if (!client || !seeded) return;
    let cancelled = false;
    client
      .call<{ seed: string }>("chat.forkSeed", { chatId: chat.ID })
      .then((result) => {
        if (!cancelled) setSeed(result.seed);
      })
      .catch(() => {
        // A daemon without the seed (or a failed fetch) reads as "no
        // summary to show" rather than an endless spinner.
        if (!cancelled) setSeed("");
      });
    return () => {
      cancelled = true;
    };
  }, [client, chat.ID, seeded]);

  const inherited = fork && runs !== null ? inheritedUpTo(runs, fork.forkedAtRunId) : null;
  const turn = inherited !== null && inherited.length > 0 ? inherited.length : null;
  return { inherited, turn, seed, loading: fork !== null && (runs === null || (seeded && seed === null)) };
}
