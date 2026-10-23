/**
 * Rate-limit wording shared by the run footer and the task lineage (ADR-0041
 * R3). Pure: callers pass `now`, and reset instants arrive as RFC 3339 UTC
 * strings that are shown in local time.
 */

/** runs.FailureKindRateLimited (ADR-0041). */
export const RATE_LIMITED = "rate_limited";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

/** True for an errored run the daemon classified as a provider rate limit. */
export function isRateLimitedRun(run: { status: string; failureKind?: string }): boolean {
  return run.status === "error" && run.failureKind === RATE_LIMITED;
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

/** Local `17:35`, prefixed with the weekday (`Mon 17:35`) when it is not the same local day as `now`. */
export function formatResetClock(reset: Date, now: Date): string {
  const hhmm = `${String(reset.getHours()).padStart(2, "0")}:${String(reset.getMinutes()).padStart(2, "0")}`;
  const sameDay = reset.getFullYear() === now.getFullYear() && reset.getMonth() === now.getMonth() && reset.getDate() === now.getDate();
  return sameDay ? hhmm : `${WEEKDAYS[reset.getDay()]} ${hhmm}`;
}

/** The tail of a rate-limit line: "resets 17:35", "limit has reset" or "reset time unknown". */
export function describeRateLimitReset(resetAt: string | undefined | null, now: Date): string {
  const reset = parseResetAt(resetAt);
  if (reset === null) return "reset time unknown";
  if (reset.getTime() <= now.getTime()) return "limit has reset";
  return `resets ${formatResetClock(reset, now)}`;
}

/** "Rate limited (5h window) · resets 17:35" -- the window part is omitted when unknown. */
export function describeRateLimit(opts: { window?: string; resetAt?: string | null }, now: Date): string {
  const window = opts.window ? ` (${opts.window} window)` : "";
  return `Rate limited${window} · ${describeRateLimitReset(opts.resetAt, now)}`;
}
