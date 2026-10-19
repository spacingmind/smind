/** Coarse buckets, largest first -- the same "biggest unit that's at least 1" rule most relative-time UIs use, so "3 days ago" doesn't read as "72 hours ago". */
const UNITS: { ms: number; label: string }[] = [
  { ms: 1000 * 60 * 60 * 24 * 365, label: "y" },
  { ms: 1000 * 60 * 60 * 24 * 30, label: "mo" },
  { ms: 1000 * 60 * 60 * 24, label: "d" },
  { ms: 1000 * 60 * 60, label: "h" },
  { ms: 1000 * 60, label: "m" },
];

/** A timestamp as the callers hold it: an ISO string (the wire), epoch ms, or a Date. */
export type TimeInput = string | number | Date;

function toMs(value: TimeInput): number {
  return value instanceof Date ? value.getTime() : typeof value === "number" ? value : new Date(value).getTime();
}

/**
 * The one "Nh ago" rendering (ADR-0032 AC19: it used to exist twice, here and in the run footer): "just now",
 * "5m ago", "3h ago", "3d ago", "2mo ago", "1y ago" -- the biggest unit that is at least 1, deliberately terse
 * (matching the sidebar's `sidebar-task-diffstat` style) rather than a full relative-time sentence.
 *
 * `shortDateAfterDays`: past that many days the age is written as a short date ("Oct 3") instead -- the run
 * footer's rule, where an absolute title carries the exact time. An unparseable string comes back as given.
 */
export function formatRelativeTime(
  value: TimeInput,
  now: number | Date = Date.now(),
  options?: { shortDateAfterDays?: number },
): string {
  const then = toMs(value);
  if (Number.isNaN(then)) return typeof value === "string" ? value : "";
  const diffMs = (now instanceof Date ? now.getTime() : now) - then;
  if (diffMs < 60_000) return "just now";
  const limit = options?.shortDateAfterDays;
  if (limit !== undefined && diffMs >= limit * 86_400_000) {
    const date = new Date(then);
    return `${date.toLocaleDateString("en-US", { month: "short" })} ${date.getDate()}`;
  }
  for (const unit of UNITS) {
    const amount = Math.floor(diffMs / unit.ms);
    if (amount >= 1) return `${amount}${unit.label} ago`;
  }
  return "just now";
}

/**
 * The terser sibling of formatRelativeTime for a row's trailing time label
 * ("now", "49m", "3h", "2d", "1mo", "1y"): the sidebar task row has ~24px
 * for it, so no "ago" and no "just". Same largest-unit-at-least-1 buckets.
 */
export function formatRelativeTimeShort(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const diffMs = now - then;
  if (diffMs < 60_000) return "now";
  for (const unit of UNITS) {
    const value = Math.floor(diffMs / unit.ms);
    if (value >= 1) return `${value}${unit.label}`;
  }
  return "now";
}
