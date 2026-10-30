import { hasFutureReset, isRateLimitedRun } from "@/lib/rate-limit";

/** The run fields limitedProvidersKey reads (structurally RunEntry; kept local so lib/ never imports hooks). */
type LimitedRun = {
  status: string;
  failureKind?: string;
  provider: string;
  failureResetAt?: string | null;
};

/**
 * The providers of a chat's runs that are rate limited with a reset still
 * ahead, sorted and comma-joined into one stable key. TaskDetailPane keys a
 * provider.list invalidation effect on this value: the key changes exactly
 * when a provider enters or leaves the limited set, so the cached cooldown
 * ("retry now") is re-read then -- not on every render.
 */
export function limitedProvidersKey(runs: readonly LimitedRun[] | null | undefined, now: Date): string {
  return (runs ?? [])
    .filter((r) => isRateLimitedRun(r) && hasFutureReset(r.failureResetAt, now))
    .map((r) => r.provider)
    .sort()
    .join(",");
}
