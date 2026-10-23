import type { RunSummary } from "@/lib/types";

function newestWithModel(runs: RunSummary[], match: (run: RunSummary) => boolean): string | undefined {
  let best: RunSummary | undefined;
  for (const run of runs) {
    if (!run.usage?.model || !match(run)) continue;
    if (!best || Date.parse(run.StartedAt) > Date.parse(best.StartedAt)) best = run;
  }
  return best?.usage?.model ?? undefined;
}

/**
 * The model a "Default" model choice last resolved to: the newest run on this chat that reported
 * one, else the newest run of the same provider. Undefined when nothing is known.
 */
export function selectObservedModel(
  runs: RunSummary[] | undefined,
  { chatId, provider }: { chatId: number | null; provider: string },
): string | undefined {
  if (!runs) return undefined;
  return (
    (chatId === null ? undefined : newestWithModel(runs, (run) => run.ChatID === chatId)) ??
    newestWithModel(runs, (run) => run.Provider === provider)
  );
}
