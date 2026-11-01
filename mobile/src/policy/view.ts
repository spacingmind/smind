// view.ts turns policy.get and policy.audit answers into what the policy screen draws, as plain data, so the
// banner, the rule stack and the decision rows are unit-testable without rendering (the inbox view.ts discipline).
// The screen is read-only: the policy file is edited on disk, never through the app.

import { relativeTime } from '../inbox/view';
import { PolicyAuditRow, PolicyRule, PolicyStatus } from './types';

/** En dash: the "nothing here" stand-in (an empty why, an unmatched rule, a missing path). */
export const DASH = '–';

export const POLICY_EDIT_NOTE = 'Edit it on disk; this screen never writes it.';

/** The one-line validity banner at the top of the screen: same texts as the web app's ValidityAlert. */
export function policyBanner(s: PolicyStatus): { tone: 'danger' | 'neutral' | 'success'; title: string; detail: string | null } {
  if (s.valid === false) return { tone: 'danger', title: 'Policy file is invalid', detail: s.error ?? null };
  if (s.exists === false) {
    return { tone: 'neutral', title: 'No policy file yet', detail: 'Every request that is not floor-denied asks you.' };
  }
  if (s.enabled) return { tone: 'success', title: 'Policy active', detail: null };
  return { tone: 'neutral', title: 'Policy file is valid but the engine is off (steward.policy.enabled)', detail: null };
}

/** A rule's matching scope as plain text ('tool Bash, argv0 git'), or a dash when the rule is unscoped. */
export function ruleScope(rule: PolicyRule): string {
  const parts: string[] = [];
  if (rule.seats?.length) parts.push(`seats ${rule.seats.join(', ')}`);
  if (rule.workspaces?.length) parts.push(`workspaces ${rule.workspaces.join(', ')}`);
  if (rule.tool) parts.push(`tool ${rule.tool}`);
  if (rule.argv0) parts.push(`argv0 ${rule.argv0}`);
  if (rule.subcommand) parts.push(`subcommand ${rule.subcommand}`);
  return parts.length > 0 ? parts.join(', ') : DASH;
}

/** The rule stack: id, action and scope as their own fields, an empty why shown as a dash. */
export function ruleRows(rules: PolicyRule[]): { id: string; action: string; why: string; scope: string }[] {
  return rules.map((rule) => ({
    id: rule.id,
    action: rule.action,
    why: rule.why || DASH,
    scope: ruleScope(rule),
  }));
}

/** One decision row of the audit stack: 'tool target' cut to 80 chars, the whole text kept in fullTarget. */
export function auditRows(
  rows: PolicyAuditRow[],
  now: number,
): { key: string; time: string; decision: string; ruleId: string; seat: string; target: string; fullTarget: string }[] {
  return rows.map((row) => {
    const fullTarget = row.target ? `${row.tool} ${row.target}` : row.tool;
    return {
      key: String(row.id),
      time: relativeTime(row.createdAt, now),
      decision: row.decision,
      ruleId: row.ruleId || DASH,
      seat: row.seat || DASH,
      target: fullTarget.length > 80 ? `${fullTarget.slice(0, 80)}…` : fullTarget,
      fullTarget,
    };
  });
}

/** The policy file's path, or a dash when policy.get did not report one. */
export function policyPath(s: PolicyStatus): string {
  return s.path || DASH;
}
