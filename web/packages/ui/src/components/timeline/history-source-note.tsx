import { useState } from "react";

import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { describeHistorySource } from "@/lib/history-source";
import { Icons } from "@/lib/icons";
import type { RunEntry } from "@/hooks/use-run-timeline";

/**
 * Where a run's shown transcript came from (ADR-0053): the trimmed summary
 * the daemon persisted, or the full history rebuilt from the provider's own
 * session. A run whose history is full (or was never described) renders
 * nothing at all -- the note exists only to explain a degraded view and to
 * offer the way out of it.
 *
 * Styled after RunErrorBox: the same rounded box, the same warning pair, and
 * `bg-surface` (the neutral well neighbouring info rows use) for the info tone.
 */
export function HistorySourceNote({ run, onLoad }: { run: RunEntry; onLoad?: (run: RunEntry) => void }) {
  const [confirming, setConfirming] = useState(false);
  const history = run.history;
  if (!history || history.source === "full") return null;
  const note = describeHistorySource(history);
  const loading = history.loading === true;
  return (
    <div
      className={note.tone === "warning" ? "flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-warning/10 px-3 py-2" : "flex flex-wrap items-center gap-x-2 gap-y-1 rounded-lg bg-surface px-3 py-2"}
      data-testid="history-source-note"
      data-source={history.source}
      data-tone={note.tone}
      data-reason={history.reason ?? ""}
    >
      {loading ? (
        <p className="min-w-0 flex-1 text-ui-sm text-foreground-muted break-words">
          Loading full history…{" "}
          <Icons.spinner aria-hidden className="inline size-3 animate-spin motion-reduce:animate-none" data-testid="history-loading" />
        </p>
      ) : (
        <>
          <p className={note.tone === "warning" ? "min-w-0 flex-1 text-ui-sm text-warning break-words" : "min-w-0 flex-1 text-ui-sm text-foreground-muted break-words"}>
            {note.message}
          </p>
          {/* No handler wired means no action to offer -- a button that does nothing reads as broken. */}
          {note.canLoad && onLoad && (
            <Button type="button" variant="outline" size="xs" data-testid="history-load-full" onClick={() => (history.loadStartsAgent ? setConfirming(true) : onLoad(run))}>
              <Icons.history aria-hidden />
              Load full history
            </Button>
          )}
        </>
      )}
      {/* "30 seconds" is native.DefaultACPTimeout (internal/history/native/rebuild.go); the mobile copy is LOAD_STARTS_AGENT_CONFIRM. Keep the three in step. */}
      {history.loadStartsAgent && onLoad && (
        <ConfirmDialog
          open={confirming}
          onOpenChange={setConfirming}
          title="Start the agent to load this history?"
          description="This starts the agent to read this run's history from its own session. It can take up to 30 seconds. No prompt is sent."
          confirmLabel="Start agent"
          pending={false}
          destructive={false}
          testId="history-load-confirm"
          confirmTestId="history-load-confirm-start"
          cancelTestId="history-load-confirm-cancel"
          onConfirm={() => {
            setConfirming(false);
            onLoad(run);
          }}
        />
      )}
    </div>
  );
}
