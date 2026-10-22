import { summarizeError } from "@/lib/error-summary";
import { cn } from "@/lib/utils";

/**
 * A failure as one line, with the raw text behind a "Details" disclosure --
 * for daemon errors that wrap git/gh argv and stderr through several layers
 * (lib/error-summary.ts). A native `<details>`: keyboard- and screen-reader-
 * operable, no state to own, and `testId` stays on the whole block so
 * `toHaveTextContent` still sees the raw text.
 */
export function ErrorWithDetails({
  message,
  prefix,
  testId,
  className,
}: {
  message: string;
  /** Leading context for the summary line ("Couldn't create the PR"). */
  prefix?: string;
  testId?: string;
  className?: string;
}) {
  const { summary, details } = summarizeError(message);
  return (
    <div className={cn("text-ui-base text-destructive", className)} data-testid={testId}>
      <p>
        {prefix ? `${prefix}: ` : ""}
        {summary}
      </p>
      {details && (
        <details className="mt-1 text-ui-sm" data-testid={testId ? `${testId}-details` : undefined}>
          <summary className="w-fit cursor-pointer select-none text-foreground-muted hover:text-foreground">Details</summary>
          <pre className="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-words rounded bg-surface px-2 py-1 font-mono text-foreground-muted">
            {details}
          </pre>
        </details>
      )}
    </div>
  );
}
