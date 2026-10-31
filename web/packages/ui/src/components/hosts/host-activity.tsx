import { useState } from "react";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { HOST_ACTIVITY_LIMIT, useFleetHostAudit } from "@/hooks/use-fleet-hosts";
import type { FleetAuditEntry, FleetHost } from "@/lib/fleet-types";
import { Icons } from "@/lib/icons";
import { formatRelativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";

function decisionTone(decision: string): StatusBadgeStatus {
  if (decision === "ok") return "success";
  if (decision === "refused") return "warning";
  return "danger";
}

/** `sha256=<64 hex> <masked prefix>` -> the first 12 hex digits and the prefix: enough to match a row, never a prompt. */
export function shortSummary(summary: string): string {
  const m = /^sha256=([0-9a-f]+)\s?(.*)$/s.exec(summary);
  if (!m) return summary;
  const rest = m[2].trim();
  return `sha256=${m[1].slice(0, 12)}${rest ? ` ${rest}` : ""}`;
}

function ActivityEntry({ entry }: { entry: FleetAuditEntry }) {
  return (
    <li className="flex flex-col gap-1 px-4 py-2" data-testid={`host-activity-row-${entry.id}`}>
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-ui-sm">
        <span className="shrink-0 text-foreground-muted" title={entry.time}>
          {formatRelativeTime(entry.time)}
        </span>
        <span
          className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-ui-xs text-foreground-muted"
          data-testid={`host-activity-dir-${entry.id}`}
        >
          {entry.direction === "in" ? "In" : "Out"}
        </span>
        <span className="min-w-0 truncate font-mono text-foreground">{entry.method}</span>
        <StatusBadge status={decisionTone(entry.decision)} data-testid={`host-activity-decision-${entry.id}`}>
          {entry.code ? `${entry.decision} · ${entry.code}` : entry.decision}
        </StatusBadge>
      </div>
      <p className="truncate font-mono text-ui-xs text-foreground-muted" title={entry.summary} data-testid={`host-activity-summary-${entry.id}`}>
        {shortSummary(entry.summary)}
      </p>
    </li>
  );
}

/**
 * A host's "Activity" row in Settings -> Hosts (ADR-0048 AC14): the latest exchanges with it, read-only, from
 * `fleet.host.audit`. Closed by default and fetched only when opened, so the page costs nothing until asked.
 * Each entry is when, which way, which method, the decision and the hash with a masked, redacted start of the
 * request; the daemon never stores a prompt and masks free-text fields, and this row renders what it is given as text.
 */
export function HostActivity({ host }: { host: FleetHost }) {
  const alias = host.alias;
  const [open, setOpen] = useState(false);
  const query = useFleetHostAudit(host.hostId, open);
  const rows = query.data ?? null;
  return (
    <div data-testid={`host-activity-${alias}`}>
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`host-activity-panel-${alias}`}
          data-testid={`host-activity-toggle-${alias}`}
          onClick={() => setOpen((v) => !v)}
          className="flex min-w-0 items-center gap-2 text-left text-ui-base text-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <Icons.chevronRight aria-hidden className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
          Activity
        </button>
        {open && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            data-testid={`host-activity-refresh-${alias}`}
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            <Icons.refresh aria-hidden className={cn(query.isFetching && "animate-spin")} />
            Refresh
          </Button>
        )}
      </div>
      {open && (
        <div id={`host-activity-panel-${alias}`} className="border-t border-border" data-testid={`host-activity-panel-${alias}`}>
          {query.isError ? (
            <div className="px-4 py-3">
              <Alert variant="error" testId={`host-activity-error-${alias}`} description={query.error instanceof Error ? query.error.message : String(query.error)} />
            </div>
          ) : rows === null ? (
            <div className="px-4 py-3">
              <LoadingRows />
            </div>
          ) : rows.length === 0 ? (
            <p className="px-4 py-3 text-ui-sm text-foreground-muted" data-testid={`host-activity-empty-${alias}`}>
              No activity with {alias} yet.
            </p>
          ) : (
            <>
              <ul className="[&>*+*]:border-t [&>*+*]:border-border" data-testid={`host-activity-list-${alias}`}>
                {rows.map((r) => (
                  <ActivityEntry key={r.id} entry={r} />
                ))}
              </ul>
              {rows.length >= HOST_ACTIVITY_LIMIT && (
                <p className="border-t border-border px-4 py-2 text-ui-xs text-foreground-muted">
                  Showing the latest {HOST_ACTIVITY_LIMIT}. Run <code>smind host audit --host {alias}</code> for more.
                </p>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
