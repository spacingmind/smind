import { formatTokens } from "@/lib/usage-format";
import type { UsageWindow } from "./types";

/** Whether percentages read as the share used or the share left. */
export type UsageDisplayAs = "used" | "remaining";

const EM_DASH = "—";

export function formatPct(n: number | null | undefined): string {
  if (n == null || Number.isNaN(n)) return EM_DASH;
  return `${Math.round(Math.max(0, Math.min(100, n)))}%`;
}

/** "31%" of the window used, or "69% left" of it, under the user's preference. */
export function formatDisplayPct(
  w: UsageWindow,
  displayAs: UsageDisplayAs,
): string {
  if (displayAs === "used") return formatPct(w.usedPct);
  const remaining = w.remainingPct ?? (w.usedPct != null ? 100 - w.usedPct : null);
  if (remaining == null) return EM_DASH;
  return `${formatPct(remaining)} left`;
}

/** "resets 2h" for a window still counting down, "resetting now" once it is due. */
export function formatResetLabel(resetsAt: string | null | undefined, now: number = Date.now()): string | null {
  if (!resetsAt) return null;
  const diffMs = new Date(resetsAt).getTime() - now;
  if (!Number.isFinite(diffMs)) return null;
  if (diffMs < 60_000) return "resetting now";
  const diffMinutes = Math.floor(diffMs / 60_000);
  const diffHours = Math.floor(diffMinutes / 60);
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays > 0) return `resets ${diffDays}d`;
  if (diffHours > 0) return `resets ${diffHours}h`;
  return `resets ${diffMinutes}m`;
}

/**
 * When a cooldown ends, in the viewer's locale and zone: "2:05 PM" today, "Oct 13, 2:05 PM" on
 * another day.
 */
export function formatCooldownUntil(untilMs: number, now: number = Date.now(), locale?: string): string {
  const until = new Date(untilMs);
  const today = until.toDateString() === new Date(now).toDateString();
  const time: Intl.DateTimeFormatOptions = { hour: "numeric", minute: "2-digit" };
  return new Intl.DateTimeFormat(locale, today ? time : { month: "short", day: "numeric", ...time }).format(until);
}

/** A balance amount as the app's language writes it: "$1,234.50", "12,345". */
export function formatAmount(value: number, unit: string, locale?: string): string {
  const tag = locale ?? undefined;
  switch (unit) {
    case "usd":
      return new Intl.NumberFormat(tag, { style: "currency", currency: "USD" }).format(value);
    case "tokens":
      return formatTokens(value, "compact");
    case "credits":
    case "requests":
      return new Intl.NumberFormat(tag, { notation: "compact", maximumFractionDigits: 1 }).format(value);
    default:
      return `${new Intl.NumberFormat(tag).format(value)} ${unit}`;
  }
}
