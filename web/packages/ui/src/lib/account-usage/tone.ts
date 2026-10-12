import type { UsageTone, UsageWindow } from "./types";

const TONES: readonly UsageTone[] = ["ok", "warning", "danger", "default"];

/**
 * The report's own tone for a window when the daemon sent one, else one
 * derived from how much of it is used. The wire's `tone` is optional
 * (AC39): an unknown string is ignored, not trusted.
 */
export function windowTone(window: UsageWindow): UsageTone {
  if (window.tone != null && TONES.includes(window.tone)) return window.tone;
  const usedPct = window.usedPct;
  if (usedPct == null) return "default";
  if (usedPct > 90) return "danger";
  if (usedPct >= 70) return "warning";
  return "ok";
}
