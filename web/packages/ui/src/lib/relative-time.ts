/** Coarse buckets, largest first -- the same "biggest unit that's at least 1" rule most relative-time UIs use, so "3 days ago" doesn't read as "72 hours ago". */
const UNITS: { ms: number; label: string }[] = [
  { ms: 1000 * 60 * 60 * 24 * 365, label: "y" },
  { ms: 1000 * 60 * 60 * 24 * 30, label: "mo" },
  { ms: 1000 * 60 * 60 * 24, label: "d" },
  { ms: 1000 * 60 * 60, label: "h" },
  { ms: 1000 * 60, label: "m" },
];

/**
 * A short "Nh ago" / "just now" rendering of an ISO timestamp, for the
 * task hover card's "last activity" line (AC5) -- deliberately terse
 * (matching the sidebar's own `sidebar-task-diffstat`'s "2f +3 -1" style)
 * rather than a full relative-time sentence.
 */
export function formatRelativeTime(iso: string, now: number = Date.now()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return iso;
  const diffMs = now - then;
  if (diffMs < 60_000) return "just now";
  for (const unit of UNITS) {
    const value = Math.floor(diffMs / unit.ms);
    if (value >= 1) return `${value}${unit.label} ago`;
  }
  return "just now";
}
