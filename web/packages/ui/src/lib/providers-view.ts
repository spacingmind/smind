import { usageCopy } from "@/lib/account-usage/copy";
import { activeCooldown } from "@/lib/account-usage/model";
import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";
import type { Account, ProviderInfo, ProviderTestResult } from "@/lib/types";

export type NavTone = "ok" | "warning" | "danger" | "neutral";

export interface ProviderNavItem {
  /** ProviderInfo.id, or "other" for the bucket of accounts no provider maps. */
  id: string;
  label: string;
  accountProvider?: string;
  kind: "oauth" | "api-key" | "cli" | "other";
  accountIds: number[];
  /** Worst account tone; "neutral" when there are no accounts or the provider is CLI-managed. */
  tone: NavTone;
  connected: boolean;
}

const TONE_ORDER: Record<NavTone, number> = { neutral: 2, ok: 3, warning: 1, danger: 0 };

export function buildProviderNav(input: {
  providers: ProviderInfo[];
  accounts: Account[];
  toneOf: (accountId: number) => NavTone | undefined;
}): ProviderNavItem[] {
  const items = input.providers.map((info): ProviderNavItem => {
    const isCli = info.kind === "cli";
    const accountIds = isCli
      ? []
      : input.accounts.filter((account) => account.provider === info.accountProvider).map((a) => a.id);
    return {
      id: info.id,
      label: info.label ?? info.id,
      accountProvider: info.accountProvider,
      kind: isCli ? "cli" : (info.credentialKind ?? "other"),
      accountIds,
      tone: worstTone(accountIds.map((id) => input.toneOf(id) ?? "neutral")),
      connected: accountIds.length > 0,
    };
  });
  const mapped = new Set(input.providers.map((info) => info.accountProvider));
  const others = input.accounts.filter((account) => !mapped.has(account.provider));
  if (others.length > 0) {
    items.push({
      id: "other",
      label: "Other accounts",
      kind: "other",
      accountIds: others.map((a) => a.id),
      tone: worstTone(others.map((a) => input.toneOf(a.id) ?? "neutral")),
      connected: true,
    });
  }
  return items;
}

function worstTone(tones: NavTone[]): NavTone {
  if (tones.length === 0) return "neutral";
  return tones.reduce((worst, tone) => (TONE_ORDER[tone] < TONE_ORDER[worst] ? tone : worst));
}

export type HealthKind = "relogin" | "cooldown" | "error" | "active" | "untested";

export interface HealthBadge {
  kind: HealthKind;
  label: string;
  detail: string | null;
  tone: NavTone;
}

export function healthBadge(input: {
  usage: AccountUsage | undefined;
  test: ProviderTestResult | undefined;
  /** Account.credentialType: decides which fix the "needs re-login" detail points at (Re-login for OAuth, Update credential otherwise). */
  credentialType?: string;
  now?: number;
  formatTime?: (ms: number) => string;
}): HealthBadge {
  const now = input.now ?? Date.now();
  const formatTime =
    input.formatTime ?? ((ms: number) => Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date(ms)));
  const { usage, test } = input;
  const problem = usage?.report?.problem?.kind;
  if (usage && (problem === "expired" || problem === "rejected")) {
    // The usage copy points at Providers (right for the Usage card); here we already are in Providers, so name the fix on the row.
    const base = (usageCopy.problem(usage, now) ?? "Login needs attention").replace(/\.?\s*Reconnect it in Providers\.?$/, "");
    const fix = input.credentialType === undefined || input.credentialType === "oauth" ? "use Re-login" : "use Update credential";
    return { kind: "relogin", label: "Needs re-login", tone: "danger", detail: `${base} — ${fix}.` };
  }
  const cooldown = activeCooldown(usage, now);
  if (cooldown) {
    return {
      kind: "cooldown",
      label: `Cooling down until ${formatTime(cooldown.untilMs)}`,
      tone: "warning",
      detail: usageCopy.cooldown(cooldown, now).headline,
    };
  }
  if (test && !test.ok) {
    return { kind: "error", label: "Error", tone: "danger", detail: test.detail };
  }
  if (usage?.report?.status === "error") {
    // The provider answered nothing usable for usage -- not a failed login or test, so a warning.
    return { kind: "error", label: "Usage unavailable", tone: "warning", detail: usageCopy.problem(usage, now) };
  }
  if (test?.ok || usage?.report?.status === "available") {
    return { kind: "active", label: "Active", tone: "ok", detail: null };
  }
  return { kind: "untested", label: "Not tested", tone: "neutral", detail: null };
}

export function mostHeadroomAccountIds(input: {
  rows: { accountId: number; provider: string; badge: HealthBadge; usage: AccountUsage | undefined }[];
}): Set<number> {
  const byProvider = new Map<string, { accountId: number; headroom: number }[]>();
  for (const row of input.rows) {
    if (row.badge.kind !== "active" || row.usage?.report?.status !== "available") continue;
    const windows: unknown = row.usage.report?.windows;
    const remaining = (Array.isArray(windows) ? windows : [])
      .filter((window): window is UsageWindow => window !== null && typeof window === "object")
      .map((window) => windowRemaining(window))
      .filter((value): value is number => value != null);
    if (remaining.length === 0) continue;
    const headroom = Math.min(...remaining);
    const candidates = byProvider.get(row.provider) ?? [];
    candidates.push({ accountId: row.accountId, headroom });
    byProvider.set(row.provider, candidates);
  }
  const best = new Set<number>();
  for (const candidates of byProvider.values()) {
    if (candidates.length < 2) continue;
    candidates.sort((a, b) => b.headroom - a.headroom || a.accountId - b.accountId);
    // A full tie has no better account to point at.
    if (candidates.every((c) => c.headroom === candidates[0].headroom)) continue;
    best.add(candidates[0].accountId);
  }
  return best;
}

function windowRemaining(window: UsageWindow): number | null {
  if (typeof window.remainingPct === "number") return window.remainingPct;
  if (typeof window.usedPct === "number") return 100 - window.usedPct;
  return null;
}

export type AccountFilter = "all" | "attention" | "active";

export function filterAccountRows<
  T extends { account: Account; badge: HealthBadge; usage: AccountUsage | undefined },
>(rows: T[], opts: { query: string; filter: AccountFilter }): T[] {
  const query = opts.query.trim().toLowerCase();
  return rows.filter((row) => {
    const haystacks = [
      row.account.label,
      row.usage?.displayName,
      row.usage?.report?.planLabel,
    ].filter((value): value is string => typeof value === "string");
    const matches = haystacks.some((value) => value.toLowerCase().includes(query));
    const kindOk =
      opts.filter === "all" ||
      (opts.filter === "attention" && (row.badge.kind === "relogin" || row.badge.kind === "cooldown" || row.badge.kind === "error")) ||
      (opts.filter === "active" && row.badge.kind === "active");
    return matches && kindOk;
  });
}

const STATUS_ORDER: Record<HealthKind, number> = { relogin: 0, cooldown: 0, error: 0, untested: 1, active: 2 };

export function sortAccountRows<T extends { account: Account; badge: HealthBadge }>(
  rows: T[],
  sort: "name" | "status",
): T[] {
  const byName = (a: T, b: T) => a.account.label.localeCompare(b.account.label, undefined, { sensitivity: "base" });
  return [...rows].sort((a, b) =>
    sort === "name" ? byName(a, b) : STATUS_ORDER[a.badge.kind] - STATUS_ORDER[b.badge.kind] || byName(a, b),
  );
}
