import { RunTimeline } from "@/components/timeline/run-timeline";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import type { RunEntry } from "@/hooks/use-run-timeline";
import type { Chat } from "@/lib/types";

/**
 * What a forked chat shows before its own first turn (ADR-0057): for a
 * native fork, the parent's turns up to the fork point (the agent session
 * branched, so those turns are still real history this chat continues
 * from); for a seeded fork, the summary the daemon gave the agent instead
 * of its own memory. Either way a divider marks where this chat starts.
 * Read-only: the inherited rows get none of the hover actions, so nothing
 * here can re-run or fork the parent's turns from this chat.
 */
export function ForkContext({
  chat,
  parentTitle,
  inherited,
  seed,
  seedFailed = false,
  loading,
  worktreePath,
}: {
  chat: Chat;
  /** The parent chat's title, for the inherited block's label. */
  parentTitle: string;
  /** useForkSource's inherited runs (the parent's turns up to the fork point). */
  inherited: RunEntry[] | null;
  /** useForkSource's seed text; shown only for a seeded fork. */
  seed: string | null;
  /** The seed could not be fetched. */
  seedFailed?: boolean;
  loading: boolean;
  worktreePath?: string;
}) {
  const fork = chat.fork;
  if (!fork) return null;
  const label = fork.kind === "retry" ? "Retry starts here" : "Forked here";

  return (
    <div className="space-y-4 pb-4">
      {loading ? (
        <InlineSpinner label="Loading earlier turns…" />
      ) : fork.mode === "native" ? (
        <section data-testid="fork-inherited" aria-label={`Earlier turns from ${parentTitle}`}>
          <div className="select-text opacity-60">
            <ul className="space-y-4">
              {(inherited ?? []).map((run) => (
                <RunTimeline key={run.id} run={run} detailLevel="overview" worktreePath={worktreePath} />
              ))}
            </ul>
          </div>
        </section>
      ) : (
        <section data-testid="fork-seed" aria-label="Summary sent to the agent">
          <div className="space-y-2 rounded-lg border border-border bg-muted/20 p-3 select-text opacity-60">
            <h3 className="text-ui-sm font-medium text-foreground">Summary sent to the agent</h3>
            <p className="text-ui-sm text-foreground-muted">
              The agent was given this account of the earlier turns instead of its own memory of them; it does not
              remember tool calls or reasoning.
            </p>
            <details>
              <summary className="cursor-pointer text-ui-sm text-foreground-muted">Show the summary</summary>
              <div className="mt-2 rounded-lg border border-border bg-background p-3">
                {seedFailed ? (
                  <p className="text-ui-sm" data-testid="fork-seed-error">
                    The summary could not be loaded.
                  </p>
                ) : (
                  <pre className="whitespace-pre-wrap text-ui-sm">{seed}</pre>
                )}
              </div>
            </details>
          </div>
        </section>
      )}
      <div data-testid="fork-divider" role="separator" aria-label={label} className="flex items-center gap-3 py-1">
        <span aria-hidden className="h-px flex-1 bg-border" />
        <span className="text-ui-sm text-foreground-muted">{label}</span>
        <span aria-hidden className="h-px flex-1 bg-border" />
      </div>
    </div>
  );
}
