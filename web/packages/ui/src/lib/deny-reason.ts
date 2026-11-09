/** The most characters a Deny's reason may carry; the daemon's runs.MaxDenyReasonRunes counts the same unit (runes). */
export const DENY_REASON_MAX = 500;

/** The counter appears from this many characters on, so a short reason shows no number. */
export const DENY_REASON_COUNTER_FROM = 400;

/** Characters the way the daemon counts them (runes): an emoji is one, not two UTF-16 units. */
export function runeCount(text: string): number {
  return Array.from(text).length;
}

/** `text` cut to at most `max` runes, never in the middle of a surrogate pair. */
export function clampRunes(text: string, max: number = DENY_REASON_MAX): string {
  const runes = Array.from(text);
  return runes.length <= max ? text : runes.slice(0, max).join("");
}

export type DenyReasonCounter = { shown: boolean; used: number; max: number; atLimit: boolean };

/** What the counter under the field shows: hidden below the threshold, "used / max" from it, and flagged at the limit. */
export function denyReasonCounter(text: string): DenyReasonCounter {
  const used = runeCount(text);
  return { shown: used >= DENY_REASON_COUNTER_FROM, used, max: DENY_REASON_MAX, atLimit: used >= DENY_REASON_MAX };
}
