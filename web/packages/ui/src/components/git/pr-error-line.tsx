import { Fragment, type ReactNode } from "react";

import { ForgeSettingsLink, isForgeSetupCode } from "@/components/git/forge-settings-link";
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
  onNavigate,
  children,
}: {
  error: PrErrorView;
  testId?: string;
  className?: string;
  /** Runs before a Settings link navigates (a dialog closes itself). */
  onNavigate?: () => void;
  children?: ReactNode;
}) {
  const { message, detail } = error;
  const setup = isForgeSetupCode(error.code);
  return (
    <div className={cn("text-ui-base text-destructive", className)} role="alert" data-testid={testId}>
      <p>{inlineCode(message)}</p>
      {(detail || children || setup) && (
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
          {children}
          {setup && <ForgeSettingsLink onNavigate={onNavigate} testId={`${testId}-settings-link`} />}
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

/** The daemon's sentences mark commands and refs with backticks: show them as code, never as markup. */
function inlineCode(text: string): ReactNode {
  return text.split(/`([^`]+)`/).map((part, i) =>
    i % 2 === 1 ? (
      <code key={i} className="rounded bg-surface px-1 font-mono text-ui-sm">
        {part}
      </code>
    ) : (
      <Fragment key={i}>{part}</Fragment>
    ),
  );
}
