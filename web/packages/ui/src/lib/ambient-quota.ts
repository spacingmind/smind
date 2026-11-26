/**
 * The ambient quota notice's key and level vocabulary (ADR-0059 F, D5): a
 * dismissal is remembered per account, window, reset (bucketed to 5 minutes
 * so a vendor's seconds of jitter is the same window) and level, so `full`
 * after a dismissed `warn` is a new notice.
 */

export type QuotaLevel = "warn" | "full";

/** `resetsAt` is rounded to this bucket when keyed: seconds of jitter is the same window. */
export const RESET_BUCKET_MS = 5 * 60 * 1000;

/** The dismissal key: `accountId|windowId|resetsAtBucket|level`. Window ids never contain `|`. */
export function quotaNoticeKey(accountId: number, windowId: string, resetsAtMs: number, level: QuotaLevel): string {
  return `${accountId}|${windowId}|${Math.round(resetsAtMs / RESET_BUCKET_MS)}|${level}`;
}

/** The reset a dismissal key's bucket stands for (ms), or null when the key is malformed. */
export function quotaKeyResetMs(key: string): number | null {
  const parts = key.split("|");
  if (parts.length !== 4) return null;
  const bucket = Number(parts[2]);
  if (!Number.isInteger(bucket) || bucket <= 0) return null;
  return bucket * RESET_BUCKET_MS;
}
