import type { ReactNode } from "react";

import { UsageList, type UsageRefreshProps } from "@/components/account-usage/usage-list";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { usageCopy } from "@/lib/account-usage/copy";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import type { UsageView } from "@/lib/account-usage/types";

/** A centered muted card holding one sentence (Paseo's UsageMessage). */
export function UsageMessage({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <div
      data-testid={testId}
      className="rounded-xl border bg-card px-4 py-6 text-center text-ui-base text-muted-foreground"
    >
      {children}
    </div>
  );
}

/** What a usage surface shows for a {@link UsageView}: a message, a spinner, an error with retry, or the list. */
export function UsageBody({
  view,
  displayAs,
  refreshing,
  failed,
  pressedAt,
  onRefreshOne,
  onRetry,
}: { view: UsageView; displayAs: UsageDisplayAs; onRetry: () => void } & UsageRefreshProps) {
  switch (view.kind) {
    case "unavailable":
    case "disabled":
      return <UsageMessage testId={`usage-${view.kind}`}>{view.message}</UsageMessage>;
    case "loading":
      return (
        <div data-testid="usage-loading" className="flex justify-center py-6">
          <InlineSpinner label={usageCopy.loading} />
        </div>
      );
    case "error":
      return (
        <Alert variant="error" testId="usage-error" title="Couldn't load plan usage" description={view.message}>
          <Button type="button" variant="outline" size="sm" onClick={onRetry}>
            Try again
          </Button>
        </Alert>
      );
    case "ready":
      if (view.reports.length === 0) return <UsageMessage testId="usage-empty-list">{usageCopy.empty}</UsageMessage>;
      return (
        <UsageList
          reports={view.reports}
          displayAs={displayAs}
          refreshing={refreshing}
          failed={failed}
          pressedAt={pressedAt}
          onRefreshOne={onRefreshOne}
        />
      );
  }
}
