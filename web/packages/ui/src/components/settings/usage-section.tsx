import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { UsageBody } from "@/components/account-usage/usage-body";
import { UsageControls } from "@/components/account-usage/usage-controls";
import { useUsageSurface } from "@/components/account-usage/use-usage-surface";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { StepGroup } from "@/components/ui/step-group";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { EM_DASH, formatCost, formatTokens, usageSince } from "@/lib/usage-format";
import type { UsageGroupBy, UsageRange, UsageScope, UsageSummaryRow } from "@/lib/types";
import { Icons } from "@/lib/icons";

const SCOPE_OPTIONS: { value: UsageScope; label: string }[] = [
  { value: "runs", label: "Runs" },
  { value: "proxy", label: "Proxy" },
  { value: "all", label: "All" },
];

const GROUP_BY_OPTIONS: { value: UsageGroupBy; label: string }[] = [
  { value: "task", label: "Task" },
  { value: "provider", label: "Provider" },
  { value: "model", label: "Model" },
  { value: "workspace", label: "Workspace" },
];

const RANGE_OPTIONS: { value: UsageRange; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
];

const GROUP_LABELS: Record<UsageGroupBy, string> = {
  task: "Task",
  provider: "Provider",
  model: "Model",
  workspace: "Workspace",
};

type TokenField = "inputTokens" | "cacheReadTokens" | "outputTokens" | "reasoningTokens";

const TOKEN_COLUMNS: { field: TokenField; label: string }[] = [
  { field: "inputTokens", label: "Input" },
  { field: "cacheReadTokens", label: "Cached" },
  { field: "outputTokens", label: "Output" },
  { field: "reasoningTokens", label: "Reasoning" },
];

/**
 * A block heading inside the Usage section. Not SettingsSectionTitle: under
 * the full-screen Settings screen that drops its title because the header
 * already names the section ("Usage"), which would leave "Plan usage" and
 * "Token usage" as two unlabeled blocks.
 */
function BlockTitle({ title, description, actions }: { title: string; description: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <div className="min-w-0">
        <h3 className="text-ui-base font-medium text-foreground">{title}</h3>
        <p className="text-ui-sm text-muted-foreground">{description}</p>
      </div>
      {actions}
    </div>
  );
}

/**
 * Settings -> Usage's "Plan usage" block (ADR-0028 PR-D1): each account's plan
 * windows, ported from Paseo's Usage settings section. It keeps its controls
 * (account filter, Refresh all, a visible Settings button) in its own body
 * rather than the Settings screen's header, and assumes nothing about the
 * container -- it renders the same in the in-shell Settings, the full-screen
 * Settings screen's framed view and its narrow detail page (AC67). While it
 * is mounted it is a touching surface (one `touch` on open, then at most one
 * per TTL while the window is visible and focused); see useUsageReports.
 */
function PlanUsageBlock() {
  const surface = useUsageSurface({ touch: true });
  return (
    <section className="flex flex-col gap-3" data-testid="settings-plan-usage">
      <BlockTitle
        title="Plan usage"
        description="Plan windows and reset times for each account"
        actions={<UsageControls surface={surface} labelledSettings />}
      />
      <UsageBody
        view={surface.view}
        displayAs={surface.displayAs}
        refreshing={surface.snapshot.refreshing}
        failed={surface.snapshot.failed}
        pressedAt={surface.snapshot.pressedAt}
        onRefreshOne={surface.refreshOne}
        onRetry={surface.retry}
        pinning={surface.pinning}
      />
    </section>
  );
}

/**
 * Settings -> Usage (ADR-0020's UI follow-up, U5): the `usage.summary`
 * table over runs/proxy requests, grouped and ranged client-side. The
 * data contract's one rule -- null/unreported NEVER means 0 -- drives the
 * cell rendering: a token field named in the row's `unreported` array is
 * an em dash, and cost is an em dash when null or the row came from the
 * proxy (which reports no cost).
 */
function UsageSection({ client }: SettingsSectionContext) {
  const [scope, setScope] = useState<UsageScope>("runs");
  const [groupBy, setGroupBy] = useState<UsageGroupBy>("task");
  const [range, setRange] = useState<UsageRange>("7d");
  const [rows, setRows] = useState<UsageSummaryRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshTick, setRefreshTick] = useState(0);
  const summarySeq = useRef(0);

  useEffect(() => {
    if (!client) return;
    const seq = ++summarySeq.current;
    setLoading(true);
    setError(null);
    client
      .call<UsageSummaryRow[]>("usage.summary", { scope, groupBy, since: usageSince(range) })
      .then((list) => {
        if (seq !== summarySeq.current) return;
        setRows(list ?? []);
        setLoading(false);
      })
      .catch((err) => {
        if (seq !== summarySeq.current) return;
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
  }, [client, scope, groupBy, range, refreshTick]);

  // Row names come from the shared workspace tree (the sidebar's own query, live via lifecycle events): no
  // workspace.list + one task.list per workspace of its own. A tree that has not loaded (or failed) just
  // leaves rows on their "Task #id" / "Workspace #id" fallbacks, as the old best-effort lookup did.
  const { workspaces } = useWorkspaceTree();
  const names = useMemo(() => {
    const resolved: Record<string, string> = {};
    if (groupBy !== "task" && groupBy !== "workspace") return resolved;
    for (const w of workspaces ?? []) {
      resolved[`workspace:${w.ID}`] = w.Title || `Workspace #${w.ID}`;
      if (groupBy !== "task") continue;
      for (const t of [...w.ungroupedTasks, ...w.spaces.flatMap((sp) => sp.tasks)]) {
        resolved[`task:${t.ID}`] = t.Title || `Task #${t.ID}`;
      }
    }
    return resolved;
  }, [workspaces, groupBy]);

  function changeScope(next: UsageScope) {
    // The proxy scope only accepts groupBy "model"; switching in one state
    // update means exactly one usage.summary per click.
    if (next === "proxy") {
      setScope(next);
      setGroupBy("model");
    } else {
      setScope(next);
    }
  }

  function labelFor(row: UsageSummaryRow): string {
    if (groupBy === "provider") return row.key;
    if (groupBy === "model") return row.key === "" ? "Unknown model" : row.key;
    const resolved = names[`${groupBy}:${row.key}`];
    if (resolved) return resolved;
    return groupBy === "task" ? `Task #${row.key}` : `Workspace #${row.key}`;
  }

  const countHeader = scope === "runs" ? "Runs" : scope === "proxy" ? "Requests" : "Count";

  function cell(row: UsageSummaryRow, field: TokenField): string {
    if (row.unreported?.includes(field)) return EM_DASH;
    return formatTokens(row[field], "full");
  }

  function costCell(row: UsageSummaryRow): string {
    if (row.scope === "proxy") return EM_DASH;
    return formatCost(row.costUsd);
  }

  return (
    <div className="flex flex-col gap-8" data-testid="settings-section-usage">
      <PlanUsageBlock />
      <section className="flex flex-col gap-3" data-testid="settings-token-usage">
        <BlockTitle
          title="Token usage"
          description="Tokens and cost per run and proxy request"
          actions={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-testid="usage-refresh"
              onClick={() => setRefreshTick((n) => n + 1)}
            >
              <Icons.refresh aria-hidden className="size-3.5" /> Refresh
            </Button>
          }
        />
        <div className="flex flex-wrap items-end gap-4">
          <div className="flex flex-col gap-1">
            <span className="text-ui-sm text-muted-foreground">Scope</span>
            <StepGroup groupLabel="usage-scope" options={SCOPE_OPTIONS} value={scope} onChange={changeScope} />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-ui-sm text-muted-foreground">Group by</span>
            <StepGroup
              groupLabel="usage-group-by"
              options={GROUP_BY_OPTIONS}
              value={groupBy}
              onChange={setGroupBy}
              isDisabled={(value) => scope === "proxy" && value !== "model"}
            />
          </div>
          <div className="flex flex-col gap-1">
            <span className="text-ui-sm text-muted-foreground">Range</span>
            <StepGroup groupLabel="usage-range" options={RANGE_OPTIONS} value={range} onChange={setRange} />
          </div>
        </div>
      </section>

      {loading ? (
        <InlineSpinner label="Loading usage…" />
      ) : error ? (
        <Alert variant="error" testId="usage-error" description={error} />
      ) : (rows ?? []).length === 0 ? (
        <EmptyState
          testId="usage-empty"
          title="No usage recorded in this range"
          description="Runs report usage when their agent does; try a longer range."
        />
      ) : (
        <div className="overflow-x-auto rounded-xl border bg-card">
          <table className="w-full text-ui-sm">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="px-3 py-2 font-normal">{GROUP_LABELS[groupBy]}</th>
                {scope === "all" && <th className="px-3 py-2 font-normal">Scope</th>}
                <th className="px-3 py-2 text-right font-normal">{countHeader}</th>
                {TOKEN_COLUMNS.map((col) => (
                  <th key={col.field} className="px-3 py-2 text-right font-normal">
                    {col.label}
                  </th>
                ))}
                <th className="px-3 py-2 text-right font-normal">Cost</th>
              </tr>
            </thead>
            <tbody>
              {(rows ?? []).map((row) => (
                <tr key={`${row.scope}-${row.key}`} data-testid={`usage-row-${row.scope}-${row.key}`} className="border-t">
                  <td className="px-3 py-2 text-foreground">{labelFor(row)}</td>
                  {scope === "all" && (
                    <td className="px-3 py-2 text-muted-foreground">
                      <span className="rounded-full border border-border px-1.5 text-ui-sm text-muted-foreground">
                        {row.scope}
                      </span>
                    </td>
                  )}
                  <td data-testid="usage-cell-count" className="px-3 py-2 text-right tabular-nums text-foreground">
                    {row.count.toLocaleString("en-US")}
                  </td>
                  {TOKEN_COLUMNS.map((col) => (
                    <td
                      key={col.field}
                      data-testid={`usage-cell-${col.field}`}
                      className="px-3 py-2 text-right tabular-nums text-foreground"
                    >
                      {cell(row, col.field)}
                    </td>
                  ))}
                  <td data-testid="usage-cell-costUsd" className="px-3 py-2 text-right tabular-nums text-foreground">
                    {costCell(row)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

registerSettingsSection({
  id: "usage",
  label: "Usage",
  groupLabel: "Agents & providers",
  order: 190,
  icon: <Icons.usage aria-hidden className="size-3.5" />,
  render: (ctx) => <UsageSection {...ctx} />,
});
