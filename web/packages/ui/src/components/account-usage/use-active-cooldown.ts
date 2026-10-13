import { useEffect, useState } from "react";

import { activeCooldown, type ActiveCooldown } from "@/lib/account-usage/model";
import type { AccountUsage } from "@/lib/account-usage/types";

/** The longest a single `setTimeout` can wait; a later `until` re-arms after each wake-up. */
const MAX_TIMEOUT_MS = 2 ** 31 - 1;

/**
 * The account's passive cooldown while it lasts. One timeout to `until` re-renders the card the
 * moment it ends, so the badge disappears by itself, with no polling and no reload. The timer is
 * replaced when `until` changes (a new cooldown event) and cleared on unmount.
 */
export function useActiveCooldown(usage: AccountUsage): ActiveCooldown | null {
  const [tick, setTick] = useState(0);
  const cooldown = activeCooldown(usage);
  const untilMs = cooldown?.untilMs ?? null;

  useEffect(() => {
    if (untilMs === null) return;
    const timer = setTimeout(() => setTick((n) => n + 1), Math.min(Math.max(untilMs - Date.now(), 0), MAX_TIMEOUT_MS));
    return () => clearTimeout(timer);
    // `tick` re-arms the timer after each wake-up (a wait longer than MAX_TIMEOUT_MS).
  }, [untilMs, tick]);

  return cooldown;
}
