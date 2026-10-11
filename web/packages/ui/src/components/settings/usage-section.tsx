import { useEffect, useRef, useState } from "react";
import { BarChart3, RefreshCw } from "lucide-react";

import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { StepGroup } from "@/components/ui/step-group";
import { EM_DASH, formatCost, formatTokens, usageSince } from "@/lib/usage-format";
import type { Task, UsageGroupBy, UsageRange, UsageScope, UsageSummaryRow, Workspace } from "@/lib/types";

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
  const [names, setNames] = useState<Record<string, string>>({});
  const summarySeq = useRef(0);
  const namesSeq = useRef(0);

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

  useEffect(() => {
    if (!client || (groupBy !== "task" && groupBy !== "workspace")) return;
    const seq = ++namesSeq.current;
    (async () => {
      try {
        const workspaces = await client.call<Workspace[]>("workspace.list");
        if (seq !== namesSeq.current) return;
        const resolved: Record<string, string> = {};
        for (const w of workspaces ?? []) resolved[`workspace:${w.ID}`] = w.Title || `Workspace #${w.ID}`;
        if (groupBy === "task") {
          for (const w of workspaces ?? []) {
            try {
              const tasks = await client.call<Task[]>("task.list", { workspaceId: w.ID });
              if (seq !== namesSeq.current) return;
              for (const t of tasks ?? []) resolved[`task:${t.ID}`] = t.Title || `Task #${t.ID}`;
            } catch {
              // Best-effort: unknown tasks fall back to "Task #id" below.
            }
          }
        }
        if (seq !== namesSeq.current) return;
        setNames(resolved);
      } catch {
        // Best-effort lookups never block or error the table.
      }
    })();
  }, [client, groupBy, refreshTick]);

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
    <div className="flex flex-col gap-6" data-testid="settings-section-usage">
      <section className="flex flex-col gap-3">
        <SettingsSectionTitle
          title="Usage"
          description="Tokens and cost per run and proxy request"
          actions={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              data-testid="usage-refresh"
              onClick={() => setRefreshTick((n) => n + 1)}
            >
              <RefreshCw aria-hidden className="size-3.5" /> Refresh
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
  icon: <BarChart3 aria-hidden className="size-3.5" />,
  render: (ctx) => <UsageSection {...ctx} />,
});
