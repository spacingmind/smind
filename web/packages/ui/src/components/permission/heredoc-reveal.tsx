import { useState } from "react";

import type { HeredocRef, HeredocReveal as HeredocRevealResult } from "@/lib/types";

/**
 * The "Show heredoc (N bytes)" toggles behind an elided `command`'s
 * `<<TAG [heredoc N bytes]` markers (the heredoc-reveal plan): the bodies
 * never ride the event stream, so each is fetched on demand through
 * `reveal` -- once, on first open -- and rendered wrapped (never clipped
 * horizontally), redacted and truncated notes included.
 */
export function HeredocReveal({
  refs,
  reveal,
  testId,
}: {
  refs: HeredocRef[];
  reveal: (index: number) => Promise<HeredocRevealResult>;
  testId: string;
}) {
  return (
    <div data-testid={testId} className="flex flex-col gap-1">
      {refs.map((hd) => (
        <HeredocRow key={hd.index} hd={hd} reveal={reveal} testId={testId} />
      ))}
    </div>
  );
}

function HeredocRow({
  hd,
  reveal,
  testId,
}: {
  hd: HeredocRef;
  reveal: (index: number) => Promise<HeredocRevealResult>;
  testId: string;
}) {
  const [open, setOpen] = useState(false);
  const [body, setBody] = useState<HeredocRevealResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function fetchBody(): Promise<void> {
    setLoading(true);
    setError(null);
    try {
      setBody(await reveal(hd.index));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  const hasBody = body !== null;
  return (
    <div>
      <button
        type="button"
        data-testid={`${testId}-toggle-${hd.index}`}
        aria-expanded={open}
        onClick={() => {
          setOpen(!open);
          // Fetch once, on first open only; a later open reuses the cached
          // body (and after a failure, Retry is the way to ask again).
          if (!open && !hasBody && error === null) void fetchBody();
        }}
        className="text-foreground-muted underline underline-offset-2 outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {open ? `Hide heredoc (${hd.bytes} bytes)` : `Show heredoc (${hd.bytes} bytes)`}
      </button>
      {open && (
        <div className="mt-1 flex flex-col gap-1">
          {loading && (
            <div aria-live="polite" className="text-foreground-muted">
              Loading...
            </div>
          )}
          {error !== null && !loading && (
            <div className="flex items-center gap-2 text-destructive">
              <span>{error}</span>
              <button
                type="button"
                onClick={() => void fetchBody()}
                className="underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                Retry
              </button>
            </div>
          )}
          {hasBody && (
            <pre
              data-testid={`${testId}-body-${hd.index}`}
              className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-md bg-surface p-2 font-mono text-ui-xs text-foreground"
            >
              {body.text}
            </pre>
          )}
          {hasBody && body.redacted && <div className="text-foreground-muted">secrets redacted</div>}
          {hasBody && body.truncated && (
            <div className="text-foreground-muted">{`truncated (${body.bytes} bytes in total)`}</div>
          )}
        </div>
      )}
    </div>
  );
}
