import { ExternalLink, UntrustedText } from "@/components/pr-panel/pr-text";
import { PRSection } from "@/components/pr-panel/pr-section";
import { PRSendButton } from "@/components/pr-panel/pr-send-button";
import { Icons } from "@/lib/icons";
import { formatDurationMs } from "@/lib/probe-format";
import type { CheckItem, CheckItemState, PRDetails } from "@/lib/types";
import { cn } from "@/lib/utils";

const HEADLINE: Record<PRDetails["checks"]["state"], string> = {
  success: "All checks passed",
  failure: "Some checks were not successful",
  pending: "Checks are running",
  none: "No checks",
};

/** "2 failing · 1 running · 7 passed": zero parts are left out. */
export function checkCounts(checks: PRDetails["checks"]): string {
  const skipped = Math.max(0, checks.total - checks.passed - checks.failed - checks.pending);
  return [
    checks.failed > 0 && `${checks.failed} failing`,
    checks.pending > 0 && `${checks.pending} running`,
    checks.passed > 0 && `${checks.passed} passed`,
    skipped > 0 && `${skipped} skipped`,
  ]
    .filter(Boolean)
    .join(" · ");
}

const STATE_LABEL: Record<CheckItemState, string> = { success: "Passed", failure: "Failed", pending: "Running", skipped: "Skipped" };

function StateIcon({ state }: { state: CheckItemState }) {
  const cls = "size-3.5";
  return (
    <span role="img" aria-label={STATE_LABEL[state]} className="flex shrink-0 items-center">
      {state === "success" && <Icons.success className={cn(cls, "text-success")} />}
      {state === "failure" && <Icons.error className={cn(cls, "text-destructive")} />}
      {state === "pending" && <Icons.spinner className={cn(cls, "animate-spin text-warning motion-reduce:animate-none")} />}
      {state === "skipped" && <Icons.statusIdle className={cn(cls, "text-foreground-subtle")} />}
    </span>
  );
}

export function PRChecksSection({
  details,
  canSend,
  checkLogPending,
  pendingCheckId,
  onSendCheck,
}: {
  details: PRDetails;
  canSend: boolean;
  checkLogPending: boolean;
  /** The check whose log is being read (the hook has one pending flag; the row that was clicked shows it). */
  pendingCheckId: string | null;
  onSendCheck: (check: CheckItem) => void;
}) {
  const { checks } = details;
  const counts = checkCounts(checks);
  return (
    <PRSection id="checks" title="Checks" summary={counts}>
      <p className="text-ui-base" data-testid="pr-checks-headline">
        {HEADLINE[checks.state]}
      </p>
      {checks.items.length === 0 ? (
        <p className="mt-1 text-ui-sm text-foreground-muted">No checks have reported for this pull request.</p>
      ) : (
        <ul className="mt-2 flex flex-col" data-testid="pr-checks-list">
          {checks.items.map((check) => (
            <li key={check.id} data-testid="pr-check-row" data-state={check.state} className="flex min-w-0 items-center gap-2 py-1">
              <StateIcon state={check.state} />
              <UntrustedText className="flex-1 text-ui-base">{check.name}</UntrustedText>
              {check.durationMs !== undefined && (
                <span className="shrink-0 font-mono text-ui-xs tabular-nums text-foreground-muted">{formatDurationMs(check.durationMs)}</span>
              )}
              <ExternalLink href={check.url} label="Open check on GitHub" testId="pr-check-link" />
              {check.state === "failure" && (
                <PRSendButton
                  label="Send to chat"
                  testId="pr-check-send"
                  disabled={!canSend}
                  pending={checkLogPending && pendingCheckId === check.id}
                  onClick={() => onSendCheck(check)}
                />
              )}
            </li>
          ))}
        </ul>
      )}
      {checks.truncated && <p className="mt-1 text-ui-sm text-foreground-muted">More checks are not shown.</p>}
    </PRSection>
  );
}
