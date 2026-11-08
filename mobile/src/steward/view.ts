// view.ts turns a steward review into what the review card draws, as plain data, so chips, gate
// rows, findings and the copyable findings text are unit-testable without rendering. Verdict and
// gate text is daemon-provided: drawn through <Text>, never interpreted. PURE -- no react-native
// import -- so vitest's plain transform can load it (same constraint as icons.ts).

import { IconName } from '../ui/icons';
import { StewardAxis, StewardCriterion, StewardFinding, StewardGate, StewardReview } from './types';

export type ChipTone = 'success' | 'warning' | 'destructive' | 'neutral';

/** The verdict as the card's chip: a label, the tone it is drawn in and its icon. Never throws, never empty. */
export function verdictChip(v: string | undefined): { text: string; tone: 'success' | 'warning' | 'destructive' | 'neutral'; icon: IconName } {
  switch (v) {
    case 'ACCEPTED':
      return { text: 'Accepted', tone: 'success', icon: 'check' };
    case 'CHANGES':
      return { text: 'Changes requested', tone: 'warning', icon: 'warning' };
    case 'BLOCKED':
      return { text: 'Blocked', tone: 'destructive', icon: 'stop' };
    case undefined:
    case '':
      return { text: 'Pending', tone: 'neutral', icon: 'info' };
    default:
      return { text: v, tone: 'neutral', icon: 'info' };
  }
}

/** The card's gate checklist: failing gates first, the rest in the daemon's order. Returns a copy; the input array is never mutated, and each bucket keeps its original order. */
export function sortGatesFailingFirst(gates: StewardGate[]): StewardGate[] {
  const failing: StewardGate[] = [];
  const rest: StewardGate[] = [];
  for (const g of gates) (g.state === 'fail' ? failing : rest).push(g);
  return [...failing, ...rest];
}

/** One row of the gate checklist: a state drawn as text, an icon and the daemon's detail when it gave one. */
export function gateRow(g: StewardGate): { id: string; stateText: string; icon: IconName; failing: boolean; detail: string | null } {
  return {
    id: g.id,
    stateText: g.state || 'unknown',
    icon: g.state === 'fail' ? 'close' : g.state === 'pass' ? 'check' : 'info',
    failing: g.state === 'fail',
    detail: g.detail || null,
  };
}

/** Findings grouped per file, in first-seen file order, each file's findings by line (the card's file sections). */
export function groupFindingsByFile(findings: StewardFinding[]): { file: string; findings: StewardFinding[] }[] {
  const groups: { file: string; findings: StewardFinding[] }[] = [];
  for (const f of findings) {
    const group = groups.find((g) => g.file === f.file);
    if (group) group.findings.push(f);
    else groups.push({ file: f.file, findings: [f] });
  }
  for (const g of groups) g.findings.sort((a, b) => a.line - b.line);
  return groups;
}

/** A finding's severity for display; the daemon may omit it. */
export function severityLabel(f: StewardFinding): string {
  return f.severity ? f.severity : 'unrated';
}

/** The findings as the plain text the Human copies to the Lead: one header line, then one line per finding. */
export function findingsAsPrompt(review: StewardReview): string {
  const lines = [
    `Steward review ${review.id} of task ${review.taskId} at ${(review.headSha ?? '').slice(0, 7)}: ${verdictChip(review.verdict).text}`,
  ];
  if (review.findings.length === 0) {
    lines.push('No findings.');
  } else {
    for (const f of review.findings) {
      const axis = f.axis ? ` [${f.axis}]` : '';
      const rule = f.axis && f.rule ? ` rule: ${f.rule}` : '';
      lines.push(`- ${f.file}:${f.line}${axis}${f.severity ? ` [${f.severity}]` : ''} ${f.why} (check: ${f.check})${rule}`);
    }
  }
  return lines.join('\n');
}

/** An axis name as text: the two known axes by name, anything else as its raw text. */
export function axisLabel(axis: string | undefined): string {
  if (axis === 'standards') return 'Standards';
  if (axis === 'spec') return 'Spec';
  return axis ? axis : 'Axis';
}

function chipIcon(tone: ChipTone): IconName {
  return tone === 'success' ? 'check' : tone === 'warning' ? 'warning' : tone === 'destructive' ? 'stop' : 'info';
}

/** A pass's chip: its seat verdict once done, otherwise where it stands. Never throws, never empty. */
export function axisStatusLabel(axis: Pick<StewardAxis, 'status' | 'seatVerdict' | 'blockedReason'>): { text: string; tone: ChipTone; icon: IconName } {
  const make = (text: string, tone: ChipTone) => ({ text, tone, icon: chipIcon(tone) });
  switch (axis.status) {
    case 'done': {
      if (!axis.seatVerdict) return make('Done', 'neutral');
      const v = verdictChip(axis.seatVerdict);
      return make(v.text, v.tone);
    }
    case 'running':
      return make('Running', 'neutral');
    case 'pending':
      return make('Pending', 'neutral');
    case 'skipped':
      return make(axis.blockedReason === 'no_spec' ? 'Skipped · no spec available' : 'Skipped', 'neutral');
    case 'blocked':
      // A pass a one-axis re-run did not request is not a failure.
      if (axis.blockedReason === 'not_run') return make('Not run', 'neutral');
      return make('Did not complete', 'destructive');
    default:
      return make(axis.status ? axis.status : 'Pending', 'neutral');
  }
}

/** Why a pass did not complete, as words; an unknown reason renders as its raw text. */
export function blockedReasonLabel(reason: string | undefined): string {
  switch (reason) {
    case 'timeout':
      return 'Timed out';
    case 'budget':
      return 'Over its budget';
    case 'crashed':
      return 'Crashed';
    case 'cancelled':
      return 'Cancelled';
    case 'scope_violation':
      return 'Scope violation';
    case 'not_run':
      return 'Not run';
    case 'no_spec':
      return 'No spec available';
    default:
      return reason ?? '';
  }
}

/** True when a pass was not requested (a one-axis re-run): the review can never be accepted. */
export function isPartialAxes(axes: { status?: string; blockedReason?: string; reason?: string }[] | undefined): boolean {
  return (axes ?? []).some((a) => a.status === 'blocked' && (a.blockedReason ?? a.reason) === 'not_run');
}

/**
 * The plain sentence the card shows instead of the daemon's combined summary when the review has
 * axes (the summary carries raw `[axis]` tags and daemon notes). Built only from the combined
 * verdict and the passes' states, never from seat text.
 */
export function combinedSentence(review: Pick<StewardReview, 'verdict' | 'axes' | 'partial'>): string {
  const axes = review.axes ?? [];
  const names = (list: StewardAxis[]) => list.map((a) => axisLabel(a.axis)).join(' and ');
  const ran = axes.filter((a) => !(a.status === 'blocked' && a.blockedReason === 'not_run'));
  if (review.partial === true || isPartialAxes(axes)) {
    return `Partial review: only the ${names(ran).toLowerCase() || 'requested'} axis ran, so it cannot be accepted.`;
  }
  const changes = axes.filter((a) => a.status === 'done' && a.seatVerdict === 'CHANGES');
  const failed = axes.filter((a) => a.status === 'blocked');
  const skipped = axes.filter((a) => a.status === 'skipped');
  switch (review.verdict) {
    case 'ACCEPTED':
      return skipped.length > 0
        ? `${names(axes.filter((a) => a.status === 'done'))} accepted the change; ${names(skipped).toLowerCase()} had nothing to judge.`
        : 'Both axes accepted the change.';
    case 'CHANGES': {
      const parts: string[] = [];
      if (changes.length > 0) parts.push(`Changes requested by ${names(changes)}.`);
      if (failed.length > 0) parts.push(`${names(failed)} did not complete.`);
      return parts.length > 0 ? parts.join(' ') : 'Changes requested.';
    }
    case 'BLOCKED':
      return 'The review was blocked.';
    default:
      return 'Review in progress.';
  }
}

/** The spec axis's checklist counts; an unknown status counts as unverifiable. */
export function criteriaCounts(criteria: StewardCriterion[] | undefined): { met: number; unmet: number; unverifiable: number } {
  const out = { met: 0, unmet: 0, unverifiable: 0 };
  for (const c of criteria ?? []) {
    if (c.status === 'met') out.met++;
    else if (c.status === 'unmet') out.unmet++;
    else out.unverifiable++;
  }
  return out;
}

/**
 * Findings grouped by axis (standards, spec, then any other axis in first-seen order, findings
 * without an axis last), each axis by file as `groupFindingsByFile` does.
 */
export function groupFindingsByAxisThenFile(
  findings: StewardFinding[],
): { axis: string | undefined; files: { file: string; findings: StewardFinding[] }[] }[] {
  const order: (string | undefined)[] = [];
  for (const f of findings) if (!order.includes(f.axis || undefined)) order.push(f.axis || undefined);
  const rank = (a: string | undefined) => (a === 'standards' ? 0 : a === 'spec' ? 1 : a === undefined ? 3 : 2);
  const sorted = order.map((a, i) => ({ a, i })).sort((x, y) => rank(x.a) - rank(y.a) || x.i - y.i);
  return sorted.map(({ a }) => ({ axis: a, files: groupFindingsByFile(findings.filter((f) => (f.axis || undefined) === a)) }));
}

/** The card's header: the verdict chip, the PR label, the short head, and the daemon's stale/summary/blocked notes when present. */
export function reviewHeader(r: StewardReview): {
  chip: ReturnType<typeof verdictChip>;
  pr: string | null;
  head7: string;
  staleNote: string | null;
  summary: string | null;
  blockedReason: string | null;
  partial: boolean;
  combined: string | null;
} {
  const hasAxes = Array.isArray(r.axes) && r.axes.length > 0;
  return {
    chip: verdictChip(r.verdict),
    pr: typeof r.prNumber === 'number' ? `PR #${r.prNumber}` : null,
    head7: (r.headSha ?? '').slice(0, 7),
    staleNote: r.stale ? `Stale: ${r.stale}` : null,
    summary: r.summary || null,
    blockedReason: r.verdict === 'BLOCKED' && r.blockedReason ? `Reason: ${r.blockedReason}` : null,
    partial: hasAxes ? r.partial === true || isPartialAxes(r.axes) : false,
    combined: hasAxes ? combinedSentence(r) : null,
  };
}

/** A frame's image source: only a png payload with data is drawable, anything else is not shown. */
export function frameSource(f: { contentType?: string; data?: string } | null | undefined): string | null {
  return f && f.contentType === 'image/png' && typeof f.data === 'string' && f.data ? `data:image/png;base64,${f.data}` : null;
}
