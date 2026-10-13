/**
 * Wire types for `account.usage` (ADR-0028 PR-C, internal/wsapi/account_usage.go
 * and internal/quota/report.go). Hand-written: the daemon is the only producer,
 * but every field a surface reads is treated as possibly missing or reshaped
 * (AC39), so the optional markers below are deliberately generous and
 * `model.ts` guards before reading.
 */

export type UsageTone = "default" | "ok" | "warning" | "danger";
export type UsageProblemKind = "expired" | "rejected" | "no_quota";
export type UsageRefreshBlocked = "floor" | "budget" | "backoff" | "rejected";
export type UsageMode = "cached" | "touch" | "refresh";
export type UsageBalanceUnit = "usd" | "credits" | "requests" | "tokens";

export interface UsageWindowScope {
  kind: "" | "model" | "surface" | "feature";
  id?: string;
  name?: string;
}

export interface UsageWindow {
  id: string;
  label: string;
  shortLabel?: string;
  scope?: UsageWindowScope | null;
  /** Flagged by the daemon as a default summary window. */
  summary?: boolean;
  usedPct?: number | null;
  remainingPct?: number | null;
  /** RFC 3339. */
  resetsAt?: string | null;
  durationSeconds?: number;
  tone?: UsageTone;
  /** "fetched" (the plan endpoint) or "observed" (rate-limit headers on a proxied request). */
  source?: string;
  /** "allowed" | "allowed_warning" | "rejected"; an observed window may carry only this. */
  status?: string;
}

export type UsageCooldownScope = "account" | "model";
export type UsageCooldownReason = "rejected" | "fallback";
export type UsageCooldownWindow = "five_hour" | "weekly" | "weekly_model" | "unified";

/** A passive cooldown: an upstream rejection seen on a proxied request (ADR-0028 PR-F). */
export interface UsageCooldown {
  /** RFC 3339 UTC: when the account (or model) may be routed again. */
  until: string;
  scope: UsageCooldownScope;
  /** Set when `scope` is "model". Client-controlled text: render it as text only. */
  model?: string;
  /** "fallback": the reset time was unknown and the daemon guessed. */
  reason: UsageCooldownReason;
  /** The window the upstream rejected, when it said. */
  window?: UsageCooldownWindow;
}

export interface UsageBalance {
  id: string;
  label: string;
  used?: number | null;
  remaining?: number | null;
  limit?: number | null;
  unit: UsageBalanceUnit | string;
}

export interface UsageDetail {
  id: string;
  label: string;
  value: string;
}

export interface UsageProblem {
  kind: UsageProblemKind;
  status?: number;
  /** RFC 3339: when the login expired. */
  expiresAt?: string;
  detail?: string;
}

/** quota.Report v2. */
export interface UsageReport {
  v?: number;
  status: "available" | "unavailable" | "error";
  planLabel?: string;
  windows?: UsageWindow[];
  balances?: UsageBalance[];
  details?: UsageDetail[];
  overageEnabled?: boolean;
  problem?: UsageProblem;
  /** Fixed vocabulary: "timeout", "rate limited", "malformed response", ... */
  error?: string;
  /** RFC 3339. */
  fetchedAt: string;
}

/** One account's usage: an `account.usage` RPC row and the event payload's `usage`. */
export interface AccountUsage {
  accountId: number;
  /** Stable `<provider>-<id>`; never changes with label or credential. */
  key: string;
  provider: string;
  /** Replaces the account label everywhere (AC58). */
  displayName: string;
  credentialType: string;
  stale: boolean;
  /** RFC 3339, rounded up to the second. */
  nextRefreshAt: string | null;
  refreshBlocked: UsageRefreshBlocked | null;
  inFlight: boolean;
  lastError: string | null;
  report: UsageReport | null;
  /** Present while a passive cooldown is in force; read through `activeCooldown`, never directly. */
  cooldown?: UsageCooldown | null;
}

export interface AccountUsageParams {
  accountIds?: number[];
  mode?: UsageMode;
}

export interface AccountUsageResult {
  enabled: boolean;
  ttlSeconds: number;
  reports: AccountUsage[];
}

export interface AccountUsageEventPayload {
  usage: AccountUsage;
}

/** What a usage surface shows (Paseo's `UsageView`, adapted). */
export type UsageView =
  | { kind: "unavailable"; message: string }
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "disabled"; message: string }
  | { kind: "ready"; reports: AccountUsage[] };
