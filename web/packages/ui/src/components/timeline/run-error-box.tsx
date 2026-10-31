import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import { isPlanLimitKind } from "@/lib/rate-limit";
import { describeRunError, resetZoneNote } from "@/lib/run-error";
import type { RunEntry } from "@/hooks/use-run-timeline";

/**
 * A failed run's error: the plain sentence, a "Details" disclosure holding
 * the stored raw text (transport chain, the provider's own wording and its
 * reset time in its own zone), and the optional "Retry with higher effort".
 */
export function RunErrorBox({ run, onRetry }: { run: RunEntry; onRetry?: (run: RunEntry) => void }) {
  const [open, setOpen] = useState(false);
  if (!run.err) return null;
  const view = describeRunError(run.err, { rateLimited: isPlanLimitKind(run.failureKind), hasReset: Boolean(run.failureResetAt) });
  const zoneNote = resetZoneNote(run.failureResetZone);
  return (
    <div className="flex flex-col gap-1 rounded-lg bg-destructive/10 px-3 py-2" data-testid="run-error-box">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <p className="min-w-0 flex-1 text-ui-sm text-destructive break-words" data-testid="run-error">
          {view.message}
        </p>
        {onRetry && (
          <Button type="button" variant="outline" size="xs" data-testid="run-retry-higher-effort" onClick={() => onRetry(run)}>
            Retry with higher effort
          </Button>
        )}
      </div>
      {view.raw !== null && (
        <>
          <button
            type="button"
            className="inline-flex w-fit items-center gap-1 text-ui-xs text-foreground-muted hover:text-foreground"
            aria-expanded={open}
            data-testid="run-error-details-toggle"
            onClick={() => setOpen((v) => !v)}
          >
            {open ? <Icons.chevronDown aria-hidden className="size-3" /> : <Icons.chevronRight aria-hidden className="size-3" />}
            Details
          </button>
          {open && (
            <div className="flex flex-col gap-1" data-testid="run-error-raw">
              <pre className="font-mono text-ui-xs whitespace-pre-wrap break-words text-foreground-muted">{view.raw}</pre>
              {zoneNote && (
                <p className="text-ui-xs text-foreground-subtle" data-testid="run-error-zone-note">
                  {zoneNote}
                </p>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
