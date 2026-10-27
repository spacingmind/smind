import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import { describeRunError } from "@/lib/run-error";
import type { RunEntry } from "@/hooks/use-run-timeline";

/**
 * A failed run's error: the plain sentence, a "Details" disclosure holding
 * the stored raw text (transport chain, the provider's own wording and its
 * zone-less reset time), and the optional "Retry with higher effort".
 */
export function RunErrorBox({ run, onRetry }: { run: RunEntry; onRetry?: (run: RunEntry) => void }) {
  const [open, setOpen] = useState(false);
  if (!run.err) return null;
  const view = describeRunError(run.err, { rateLimited: run.failureKind === "rate_limited", hasReset: Boolean(run.failureResetAt) });
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
              <p className="text-ui-xs text-foreground-subtle">As reported by the provider. A time in this text is in the provider&apos;s zone, not yours.</p>
            </div>
          )}
        </>
      )}
    </div>
  );
}
