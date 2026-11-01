// permissionPolicyLine.ts turns a permission request's policy footprint (detail.policy, ADR-0047 §2) into the
// data for the permission card: which rule matched, its why, and the FULL normalised command the engine judged --
// never truncated, trimmed or ellipsised, so what the daemon saw is exactly what the human sees.

import type { PermissionDetail } from '../permissionRequests';

export interface PermissionPolicyLine {
  title: string;
  ruleId: string | null;
  why: string | null;
  command: string | null;
}

/** Null when the request carries no policy footprint; the card then falls back to the raw command. */
export function permissionPolicyLine(detail: PermissionDetail | undefined): PermissionPolicyLine | null {
  const policy = detail?.policy;
  if (!policy) return null;
  const ruleId = policy.ruleId || null;
  return {
    title: ruleId ? `Matched rule ${ruleId}` : 'No rule matched',
    ruleId,
    why: ruleId && policy.why ? policy.why : null,
    command: policy.normalized || null,
  };
}

/** The command line under the buttons: the policy's normalised form when present, else the raw command, in full either way. */
export function permissionCommand(detail: PermissionDetail | undefined): string | null {
  return detail?.policy?.normalized || detail?.command || null;
}
