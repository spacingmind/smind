import { useEffect, useRef, useState } from "react";

import { PrErrorLine } from "@/components/git/pr-error-line";
import { PRChecksSection } from "@/components/pr-panel/pr-checks-section";
import { PRHeader } from "@/components/pr-panel/pr-header";
import { PRNotice } from "@/components/pr-panel/pr-notice";
import { PRReviewsSection } from "@/components/pr-panel/pr-reviews-section";
import { PRThreadsSection } from "@/components/pr-panel/pr-threads-section";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { describePrError } from "@/components/git/pr-error";
import { usePRDetails, useRefreshPRDetails } from "@/hooks/use-pr-details";
import { useSendPRToChat } from "@/hooks/use-send-pr-to-chat";
import { useTaskGit } from "@/hooks/use-task-git";
import { Icons } from "@/lib/icons";
import type { CheckItem, PRDetails } from "@/lib/types";

/** The daemon served an empty cache (nothing fetched yet) rather than a pull request. */
function hasNoContent(d: PRDetails): boolean {
  return d.title === "" && d.checks.total === 0 && d.reviews.items.length === 0 && d.threads.items.length === 0;
}

/**
 * While the daemon cannot fetch (budget / backoff) and there is nothing to show yet, refetch once `refreshAvailableAt` has
 * passed, and again after every refetch that still ends waiting: the daemon rounds that time down to the second and
 * answers with the same value while it is still inside its floor, so the time alone never changes. The timer is armed 1 s
 * after the later of that time and now (never early, never faster than once a second); cleared on blur or unmount.
 */
const REARM_MS = 1000;
function useRefetchWhenAvailable(details: PRDetails | undefined, focused: boolean, refetch: () => unknown, updatedAt: number) {
  const refetchRef = useRef(refetch);
  refetchRef.current = refetch;
  const waiting = details !== undefined && (details.stale === "budget" || details.stale === "backoff") && hasNoContent(details);
  const at = waiting ? Date.parse(details.refreshAvailableAt) : NaN;
  useEffect(() => {
    if (!focused || Number.isNaN(at)) return;
    // `updatedAt` is a dependency on purpose: each completed refetch re-arms this, even with an identical answer.
    const timer = setTimeout(() => void refetchRef.current(), Math.max(0, at - Date.now()) + REARM_MS);
    return () => clearTimeout(timer);
  }, [focused, at, updatedAt]);
}

const ERROR_DISMISS_MS = 8000;

/**
 * Clears the Refresh error by itself: when its `retryAt` passes, else after 8 s. (A new answer or the next refresh clears it
 * too; see the pane.) The timer is cleared on blur and unmount.
 */
function useDismissRefreshError(error: { retryAt?: string } | null, focused: boolean, dismiss: () => void) {
  const dismissRef = useRef(dismiss);
  dismissRef.current = dismiss;
  const hasError = error !== null;
  const retryAt = error?.retryAt;
  useEffect(() => {
    if (!hasError || !focused) return;
    const at = retryAt ? Date.parse(retryAt) : NaN;
    const delay = Number.isNaN(at) ? ERROR_DISMISS_MS : Math.max(0, at - Date.now());
    const timer = setTimeout(() => dismissRef.current(), delay);
    return () => clearTimeout(timer);
  }, [hasError, retryAt, focused]);
}

function PanelSkeleton({ waitingUntil }: { waitingUntil?: string }) {
  const at = waitingUntil === undefined ? NaN : Date.parse(waitingUntil);
  return (
    <div role="status" data-testid="pr-skeleton" className="flex flex-col gap-3 p-4">
      <span className="sr-only">Loading pull request…</span>
      {waitingUntil !== undefined && (
        <p className="text-ui-sm text-foreground-muted" data-testid="pr-waiting">
          {Number.isNaN(at) ? "Waiting for GitHub…" : `Waiting for GitHub… loading again at ${new Date(at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" })}`}
        </p>
      )}
      <Skeleton aria-hidden className="h-5 w-3/4" />
      <Skeleton aria-hidden className="h-4 w-1/3" />
      <Skeleton aria-hidden className="h-24 w-full" />
      <Skeleton aria-hidden className="h-16 w-full" />
    </div>
  );
}

/** The PR tab (G9): header, checks, reviews and threads of the task's pull request, with "Send to chat". `focused` is "this tab is in front". */
export function PRPanelPane({ taskId, focused }: { taskId: number; focused: boolean }) {
  const git = useTaskGit(taskId);
  const query = usePRDetails(taskId, { focused });
  const refresh = useRefreshPRDetails(taskId);
  const send = useSendPRToChat(taskId);
  const [pendingCheckId, setPendingCheckId] = useState<string | null>(null);
  const details = query.data;
  useRefetchWhenAvailable(details, focused, query.refetch, query.dataUpdatedAt);
  useDismissRefreshError(refresh.limited ?? refresh.error, focused, refresh.reset);
  // A newer answer supersedes the refresh error.
  const resetRefresh = refresh.reset;
  useEffect(() => resetRefresh(), [query.dataUpdatedAt, resetRefresh]);

  const sendCheck = (check: CheckItem) => {
    if (!details) return;
    setPendingCheckId(check.id);
    void send.sendCheck(details, check).finally(() => setPendingCheckId(null));
  };

  let body;
  if (!details && git !== undefined && git.pr === undefined) {
    body = (
      <EmptyState
        testId="pr-empty"
        icon={<Icons.pullRequest aria-hidden className="size-5" />}
        title="No pull request yet"
        description="Create a pull request for this task to see its details here."
      />
    );
  } else if (!details && query.isError) {
    body = (
      <div className="p-4">
        <PrErrorLine error={describePrError(query.error, "details")} testId="pr-query-error">
          <Button type="button" variant="link" size="sm" className="h-auto p-0 text-ui-sm" onClick={() => void query.refetch()}>
            Try again
          </Button>
        </PrErrorLine>
      </div>
    );
  } else if (!details) {
    body = <PanelSkeleton />;
  } else if ((details.stale === "budget" || details.stale === "backoff") && hasNoContent(details)) {
    // The daemon is waiting out a floor with nothing cached yet: there is no "last data" to show, and one refetch is scheduled.
    body = <PanelSkeleton waitingUntil={details.refreshAvailableAt} />;
  } else {
    body = (
      <>
        <PRHeader details={details} onRefresh={() => refresh.refresh()} refreshing={refresh.isPending} settled={refresh.limited !== null} />
        <PRNotice details={details} />
        {(refresh.error || (query.isError && query.error)) && (
          <div className="border-b px-4 py-2">
            <PrErrorLine error={refresh.error ?? describePrError(query.error, "details")} testId="pr-refresh-error" />
          </div>
        )}
        {hasNoContent(details) ? (
          <p className="px-4 py-3 text-ui-sm text-foreground-muted">Nothing to show yet.</p>
        ) : (
          <>
            <PRChecksSection
              details={details}
              canSend={send.canSend}
              checkLogPending={send.checkLogPending}
              pendingCheckId={pendingCheckId}
              onSendCheck={sendCheck}
            />
            <PRReviewsSection details={details} />
            <PRThreadsSection
              details={details}
              canSend={send.canSend}
              onSendThread={(t) => send.sendThread(details, t)}
              onSendAll={() => send.sendAllUnresolved(details)}
            />
          </>
        )}
      </>
    );
  }

  return (
    <div data-testid="pr-panel" className="flex h-full min-h-0 min-w-0 flex-col overflow-y-auto overflow-x-hidden">
      {body}
    </div>
  );
}
