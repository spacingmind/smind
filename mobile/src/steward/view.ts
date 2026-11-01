// view.ts turns a steward review into what the review card draws, as plain data, so chips, gate
// rows, findings and the copyable findings text are unit-testable without rendering. Verdict and
// gate text is daemon-provided: drawn through <Text>, never interpreted. PURE -- no react-native
// import -- so vitest's plain transform can load it (same constraint as icons.ts).

import { IconName } from '../ui/icons';
import { StewardFinding, StewardGate, StewardReview } from './types';

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
    `Steward review ${review.id} of task ${review.taskId} at ${review.headSha.slice(0, 7)}: ${verdictChip(review.verdict).text}`,
  ];
  if (review.findings.length === 0) {
    lines.push('No findings.');
  } else {
    for (const f of review.findings) {
      lines.push(`- ${f.file}:${f.line}${f.severity ? ` [${f.severity}]` : ''} ${f.why} (check: ${f.check})`);
    }
  }
  return lines.join('\n');
}

/** The card's header: the verdict chip, the PR label, the short head, and the daemon's stale/summary/blocked notes when present. */
export function reviewHeader(r: StewardReview): {
  chip: ReturnType<typeof verdictChip>;
  pr: string | null;
  head7: string;
  staleNote: string | null;
  summary: string | null;
  blockedReason: string | null;
} {
  return {
    chip: verdictChip(r.verdict),
    pr: typeof r.prNumber === 'number' ? `PR #${r.prNumber}` : null,
    head7: r.headSha.slice(0, 7),
    staleNote: r.stale ? `Stale: ${r.stale}` : null,
    summary: r.summary || null,
    blockedReason: r.verdict === 'BLOCKED' && r.blockedReason ? r.blockedReason : null,
  };
}

/** A frame's image source: only a png payload with data is drawable, anything else is not shown. */
export function frameSource(f: { contentType?: string; data?: string } | null | undefined): string | null {
  return f && f.contentType === 'image/png' && typeof f.data === 'string' && f.data ? `data:image/png;base64,${f.data}` : null;
}
