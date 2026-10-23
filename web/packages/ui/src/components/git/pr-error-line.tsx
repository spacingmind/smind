import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import type { PrErrorView } from "@/components/git/pr-error";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * A failed PR call as ONE sentence, with the daemon's scrubbed detail behind a copyable "Details" disclosure.
 * `children` are the sentence's follow-up actions (View PR for `pr_exists`). Everything renders as text.
 */
export function PrErrorLine({
  error,
  testId = "pr-error",
  className,
  children,
}: {
  error: PrErrorView;
  testId?: string;
  className?: string;
  children?: ReactNode;
}) {
  const { message, detail } = error;
  return (
    <div className={cn("text-ui-base text-destructive", className)} role="alert" data-testid={testId}>
      <p>{message}</p>
      {(detail || children) && (
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
          {children}
          {detail && (
            <details className="text-ui-sm" data-testid={`${testId}-details`}>
              <summary className="w-fit cursor-pointer select-none text-foreground-muted hover:text-foreground">Details</summary>
              <div className="mt-1 flex items-start gap-1">
                <pre className="max-h-40 flex-1 overflow-auto whitespace-pre-wrap break-words rounded bg-surface px-2 py-1 font-mono text-foreground-muted">
                  {detail}
                </pre>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Copy details"
                  data-testid={`${testId}-copy`}
                  onClick={() => void navigator.clipboard?.writeText(detail)}
                >
                  <Icons.copy aria-hidden />
                </Button>
              </div>
            </details>
          )}
        </div>
      )}
    </div>
  );
}
