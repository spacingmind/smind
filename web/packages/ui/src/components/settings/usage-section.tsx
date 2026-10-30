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
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { EM_DASH, formatCost, formatTokens, usageSince } from "@/lib/usage-format";
import type { UsageGroupBy, UsageRange, UsageScope, UsageSummaryRow } from "@/lib/types";
import { Icons } from "@/lib/icons";
import { providerLabel } from "@/lib/provider-catalog";

const SCOPE_OPTIONS: { value: UsageScope; label: string }[] = [
  { value: "runs", label: "Runs" },
  { value: "proxy", label: "Proxy" },
  { value: "all", label: "All" },
];

/**
 * The table's group-by choices: the wire's, plus "space", which usage.summary cannot group by. Space is
 * rolled up client-side from task rows (see rollUpBySpace), so it is never sent.
 */
type UiGroupBy = UsageGroupBy | "space";

const GROUP_BY_OPTIONS: { value: UiGroupBy; label: string }[] = [
  { value: "task", label: "Task" },
  { value: "provider", label: "Provider" },
  { value: "model", label: "Model" },
  { value: "workspace", label: "Workspace" },
  { value: "space", label: "Space" },
];

const SUBSCRIPTION_COST_HINT = "Estimated at API list prices; subscription plans are not billed per token";

const RANGE_OPTIONS: { value: UsageRange; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "7d", label: "7 days" },
  { value: "30d", label: "30 days" },
];

const GROUP_LABELS: Record<UiGroupBy, string> = {
  task: "Task",
  provider: "Provider",
  model: "Model",
  workspace: "Workspace",
  space: "Space",
};

const NO_SPACE_KEY = "space:none";
const TOKEN_FIELDS: NonNullable<UsageSummaryRow["unreported"]> = [
  "inputTokens",
  "outputTokens",
  "cacheReadTokens",
  "cacheWriteTokens",
  "reasoningTokens",
];

/**
 * Sums task rows into one row per space. `spaceOf` maps a task key to its space key (tasks it does not know
 * land in "space:none"). Cost is the sum of the reported costs (null when no row reported one), a token
 * field is unreported only when every rolled row lists it, and providers are the union.
 */
function rollUpBySpace(rows: UsageSummaryRow[], spaceOf: (taskKey: string) => string, labelOf: (spaceKey: string) => string): UsageSummaryRow[] {
  const groups = new Map<string, UsageSummaryRow[]>();
  for (const row of rows) {
    const key = spaceOf(row.key);
    groups.set(key, [...(groups.get(key) ?? []), row]);
  }
  const rolled = [...groups].map(([key, members]): UsageSummaryRow => {
    const costs = members.flatMap((r) => (r.costUsd == null ? [] : [r.costUsd]));
    const providers = [...new Set(members.flatMap((r) => r.providers ?? []))].sort();
    const unreported = TOKEN_FIELDS.filter((f) => members.every((r) => r.unreported?.includes(f)));
    return {
      scope: "runs",
      key,
      count: members.reduce((n, r) => n + r.count, 0),
      inputTokens: members.reduce((n, r) => n + r.inputTokens, 0),
      outputTokens: members.reduce((n, r) => n + r.outputTokens, 0),
      cacheReadTokens: members.reduce((n, r) => n + r.cacheReadTokens, 0),
      cacheWriteTokens: members.reduce((n, r) => n + r.cacheWriteTokens, 0),
      reasoningTokens: members.reduce((n, r) => n + r.reasoningTokens, 0),
      costUsd: costs.length === 0 ? null : costs.reduce((n, c) => n + c, 0),
      ...(unreported.length > 0 && { unreported }),
      ...(providers.length > 0 && { providers }),
    };
  });
  return rolled.sort((a, b) => (b.costUsd ?? -1) - (a.costUsd ?? -1) || labelOf(a.key).localeCompare(labelOf(b.key)));
}

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
  const [groupBy, setGroupBy] = useState<UiGroupBy>("task");
  const [range, setRange] = useState<UsageRange>("7d");
  // The wire's groupBy: Space reuses the task rows and is rolled up below, so Task <-> Space never refetches.
  const apiGroupBy: UsageGroupBy = groupBy === "space" ? "task" : groupBy;
  const [fetched, setFetched] = useState<{ groupBy: UsageGroupBy; rows: UsageSummaryRow[] } | null>(null);
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
      .call<UsageSummaryRow[]>("usage.summary", { scope, groupBy: apiGroupBy, since: usageSince(range) })
      .then((list) => {
        if (seq !== summarySeq.current) return;
        setFetched({ groupBy: apiGroupBy, rows: list ?? [] });
        setLoading(false);
      })
      .catch((err) => {
        if (seq !== summarySeq.current) return;
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
  }, [client, scope, apiGroupBy, range, refreshTick]);

  // Row names come from the shared workspace tree (the sidebar's own query, live via lifecycle events): no
  // workspace.list + one task.list per workspace of its own. A tree that has not loaded (or failed) just
  // leaves rows on their "Task #id" / "Workspace #id" fallbacks, as the old best-effort lookup did.
  const { workspaces } = useWorkspaceTree();
  // Providers with a browser OAuth login (claude-native, codex-native) are subscription plans: their cost is an API-price estimate. API-key and external-CLI providers (GLM, Kimi) are metered.
  const { providers: providerList } = useProviderCatalog();
  const subscriptionProviders = useMemo(
    () => new Set(providerList.filter((p) => p.credentialKind === "oauth" || p.extends !== undefined).map((p) => p.id as string)),
    [providerList],
  );
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

  // Task id -> its space, and each space's display title (the workspace's in parentheses when two share one).
  const spaces = useMemo(() => {
    const spaceOfTask = new Map<string, string>();
    const titles = new Map<string, string>();
    const all = (workspaces ?? []).flatMap((w) => w.spaces.map((sp) => ({ w, sp })));
    for (const { w, sp } of all) {
      const key = `space:${sp.ID}`;
      const shared = all.filter((o) => o.sp.Title === sp.Title).length > 1;
      titles.set(key, shared ? `${sp.Title} (${w.Title})` : sp.Title);
      for (const t of sp.tasks) spaceOfTask.set(String(t.ID), key);
    }
    return { spaceOfTask, titles };
  }, [workspaces]);

  const rows = useMemo(() => {
    if (!fetched || groupBy !== "space") return fetched?.rows ?? null;
    // Rows of another groupBy (the last fetch, until the task rows arrive) are not task rows to roll up.
    if (fetched.groupBy !== "task") return null;
    return rollUpBySpace(
      fetched.rows,
      (taskKey) => spaces.spaceOfTask.get(taskKey) ?? NO_SPACE_KEY,
      (key) => (key === NO_SPACE_KEY ? "No space" : (spaces.titles.get(key) ?? key)),
    );
  }, [fetched, groupBy, spaces]);

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
    if (groupBy === "provider") return providerLabel(row.key, providerList);
    if (groupBy === "model") return row.key === "" ? "Unknown model" : row.key;
    if (groupBy === "space") return row.key === NO_SPACE_KEY ? "No space" : (spaces.titles.get(row.key) ?? row.key);
    const resolved = names[`${groupBy}:${row.key}`];
    if (resolved) return resolved;
    return groupBy === "task" ? `Task #${row.key}` : `Workspace #${row.key}`;
  }

  const countHeader = scope === "runs" ? "Runs" : scope === "proxy" ? "Requests" : "Count";

  function cell(row: UsageSummaryRow, field: TokenField): string {
    if (row.unreported?.includes(field)) return EM_DASH;
    return formatTokens(row[field], "full");
  }

  function costCell(row: UsageSummaryRow): ReactNode {
    if (row.scope === "proxy") return EM_DASH;
    const cost = formatCost(row.costUsd);
    if (row.costUsd == null || !row.providers?.some((id) => subscriptionProviders.has(id))) return cost;
    return (
      <span className="whitespace-nowrap">
        {cost}{" "}
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <span tabIndex={0} data-testid="usage-cost-estimate" className="text-foreground-muted">
                ≈ API price
              </span>
            </TooltipTrigger>
            <TooltipContent>{SUBSCRIPTION_COST_HINT}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </span>
    );
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
              wrap
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
