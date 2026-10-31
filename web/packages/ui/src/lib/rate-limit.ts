/**
 * Rate-limit wording shared by the run footer and the task lineage (ADR-0041
 * R3). Pure: callers pass `now`, and reset instants arrive as RFC 3339 UTC
 * strings that are shown in local time.
 */

/** runs.FailureKindRateLimited (ADR-0041). */
export const RATE_LIMITED = "rate_limited";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** The plan-window class (run failure classes plan): the old rate_limited rows with a window or reset. */
export const PLAN_WINDOW_EXHAUSTED = "plan_window_exhausted";

/** True for a plan limit: the plan window, or the residual unclassifiable rate limit. */
export function isPlanLimitKind(kind: string | undefined | null): boolean {
  return kind === PLAN_WINDOW_EXHAUSTED || kind === RATE_LIMITED;
}

/** True for an errored run the daemon classified as a provider plan limit (plan window or residual rate limit). */
export function isRateLimitedRun(run: { status: string; failureKind?: string }): boolean {
  return run.status === "error" && isPlanLimitKind(run.failureKind);
}

/** A valid reset instant, or null when absent/unparseable. */
export function parseResetAt(resetAt: string | undefined | null): Date | null {
  if (!resetAt) return null;
  const d = new Date(resetAt);
  return Number.isNaN(d.getTime()) ? null : d;
}

/** True while a reset instant is still ahead of `now` (what the footer's minute ticker keys on). */
export function hasFutureReset(resetAt: string | undefined | null, now: Date): boolean {
  const d = parseResetAt(resetAt);
  return d !== null && d.getTime() > now.getTime();
}

/** Local `17:35` (`≈17:35` when the daemon inferred the reset zone), prefixed with the weekday (`Mon 17:35`) when it is not the same local day as `now`. */
export function formatResetClock(reset: Date, now: Date, inferred = false): string {
  const hhmm = `${inferred ? "≈" : ""}${String(reset.getHours()).padStart(2, "0")}:${String(reset.getMinutes()).padStart(2, "0")}`;
  const sameDay = reset.getFullYear() === now.getFullYear() && reset.getMonth() === now.getMonth() && reset.getDate() === now.getDate();
  return sameDay ? hhmm : `${WEEKDAYS[reset.getDay()]} ${hhmm}`;
}

/** The tail of a rate-limit line: "resets 17:35", "limit has reset" or "reset time unknown". */
export function describeRateLimitReset(resetAt: string | undefined | null, now: Date, inferred = false): string {
  const reset = parseResetAt(resetAt);
  if (reset === null) return "reset time unknown";
  if (reset.getTime() <= now.getTime()) return "limit has reset";
  return `resets ${formatResetClock(reset, now, inferred)}`;
}

/** "Rate limited (5h window) · resets 17:35" -- the window part is omitted when unknown. */
export function describeRateLimit(opts: { window?: string; resetAt?: string | null; inferred?: boolean }, now: Date): string {
  const window = opts.window ? ` (${opts.window} window)` : "";
  return `Rate limited${window} · ${describeRateLimitReset(opts.resetAt, now, opts.inferred)}`;
}

/** Slack after the reset before a held retry fires, so the provider's window has certainly rolled over. */
export const RETRY_NOT_BEFORE_SLACK_MS = 30_000;

/** "Wait until 17:35" (weekday prefix on another day); null when the reset is absent or already past. */
export function waitUntilLabel(resetAt: string | undefined | null, now: Date, inferred = false): string | null {
  const reset = parseResetAt(resetAt);
  if (reset === null || reset.getTime() <= now.getTime()) return null;
  return `Wait until ${formatResetClock(reset, now, inferred)}`;
}

/** The `run.start` notBefore for a held retry: the reset instant + 30 s as RFC 3339 UTC; null when unparseable. */
export function retryNotBefore(resetAt: string | undefined | null): string | null {
  const reset = parseResetAt(resetAt);
  return reset === null ? null : new Date(reset.getTime() + RETRY_NOT_BEFORE_SLACK_MS).toISOString();
}
