import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { Button } from "@/components/ui/button";
import { UntrustedText, ExternalLink } from "@/components/pr-panel/pr-text";
import { useNow } from "@/hooks/use-now";
import { Icons } from "@/lib/icons";
import { formatRelativeTime } from "@/lib/relative-time";
import type { PRDetails } from "@/lib/types";

function stateBadge(d: Pick<PRDetails, "state" | "draft">): { label: string; status: StatusBadgeStatus } {
  if (d.state === "merged") return { label: "Merged", status: "neutral" };
  if (d.state === "closed") return { label: "Closed", status: "danger" };
  return d.draft ? { label: "Draft", status: "neutral" } : { label: "Open", status: "success" };
}

/** The zero time (`0001-01-01…`, what an empty cache carries) and a missing value are "never fetched", not a very old fetch. */
function hasFetchTime(fetchedAt: string | undefined): fetchedAt is string {
  const at = fetchedAt ? Date.parse(fetchedAt) : NaN;
  return !Number.isNaN(at) && new Date(at).getUTCFullYear() > 1970;
}

/** Title, state, `#N`, "Open on GitHub", Refresh, and how old the data is. */
export function PRHeader({
  details,
  onRefresh,
  refreshing,
}: {
  details: PRDetails;
  onRefresh: () => void;
  refreshing: boolean;
}) {
  const now = useNow();
  const badge = stateBadge(details);
  return (
    <header data-testid="pr-header" className="flex min-w-0 flex-col gap-1 border-b px-4 py-2.5">
      <div className="flex min-w-0 items-start gap-2">
        <h2 className="min-w-0 flex-1 text-ui-base font-medium">
          <UntrustedText className="line-clamp-2">{details.title}</UntrustedText>
        </h2>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="Refresh"
          title="Refresh"
          disabled={refreshing}
          aria-busy={refreshing || undefined}
          onClick={onRefresh}
          data-testid="pr-refresh"
        >
          {refreshing ? <Icons.spinner aria-hidden className="animate-spin motion-reduce:animate-none" data-testid="pr-refresh-spinner" /> : <Icons.refresh aria-hidden />}
        </Button>
      </div>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-ui-sm text-foreground-muted">
        <StatusBadge status={badge.status} data-testid="pr-state">
          {badge.label}
        </StatusBadge>
        <span className="font-mono tabular-nums">#{details.number}</span>
        <ExternalLink href={details.url} label="Open on GitHub" testId="pr-open-link">
          <span>Open on GitHub</span>
          <Icons.externalLink aria-hidden className="size-3" />
        </ExternalLink>
        {hasFetchTime(details.fetchedAt) && (
          <span className="ml-auto text-foreground-subtle" data-testid="pr-fetched">
            Updated {formatRelativeTime(details.fetchedAt, now)}
          </span>
        )}
      </div>
    </header>
  );
}
