/**
 * Wire shapes and pure helpers of the steward review card (`steward.get`, ADR-0047 sections
 * 5, 8 and 9). The daemon computes every gate and clamps the verdict; the card only renders.
 */

/** The verdict enum the seat submits (ADR-0047 §3.5); the stored verdict may also be empty while a review runs. */
export type StewardVerdict = "ACCEPTED" | "CHANGES" | "BLOCKED";

/** One finding of a `CHANGES` review: where, why, and the exact check that would show it fixed. */
export interface StewardFinding {
  file: string;
  line: number;
  why: string;
  check: string;
  severity?: string;
}

/** One objective gate, each `pass | fail | unknown | n/a` (ADR-0047 §3.3). */
export interface StewardGate {
  id: string;
  state: string;
  detail?: string;
}

/** One evidence frame the seat had to look at (the qaframes set, judged fresh at the head sha). */
export interface StewardFrame {
  name: string;
  viewport: number;
  theme: string;
  capturedSha: string;
}

/** `steward.get`'s answer (internal/steward View): commit-bound, stale-aware, never mergeable from here. */
export interface StewardReview {
  id: number;
  taskId: number;
  triggerKind: string;
  headSha: string;
  validationId: number;
  ciState: string;
  status: string;
  verdict?: string;
  summary?: string;
  blockedReason?: string;
  findings: StewardFinding[];
  gates: StewardGate[];
  frames?: StewardFrame[];
  prNumber?: number;
  stale?: string;
  mergeReady: boolean;
  notReadyReason?: string;
  scopeViolation?: boolean;
  costUsd: number;
  createdAt: string;
  finishedAt?: string;
}

/** The verdict as the card's chip: a label plus the tone it is drawn in. Never throws, never empty. */
export function verdictLabel(v: string | undefined): { text: string; tone: "success" | "warning" | "danger" | "neutral" } {
  switch (v) {
    case "ACCEPTED":
      return { text: "Accepted", tone: "success" };
    case "CHANGES":
      return { text: "Changes requested", tone: "warning" };
    case "BLOCKED":
      return { text: "Blocked", tone: "danger" };
    default:
      return { text: v ? v : "Pending", tone: "neutral" };
  }
}

/**
 * The card's gate checklist (ADR-0047 §9): failing gates first, the rest in the daemon's order.
 * Returns a copy; the input array is never mutated, and each bucket keeps its original order.
 */
export function sortGatesFailingFirst(gates: StewardGate[]): StewardGate[] {
  const failing: StewardGate[] = [];
  const rest: StewardGate[] = [];
  for (const g of gates) (g.state === "fail" ? failing : rest).push(g);
  return [...failing, ...rest];
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

/**
 * The findings as the plain text the Human pastes to the Lead (ADR-0047 §5: findings reach the
 * Lead only when the Human copies them). One header line, then one line per finding.
 */
export function findingsAsPrompt(review: StewardReview): string {
  const lines = [
    `Steward review ${review.id} of task ${review.taskId} at ${review.headSha.slice(0, 7)}: ${verdictLabel(review.verdict).text}`,
  ];
  if (review.findings.length === 0) {
    lines.push("No findings.");
  } else {
    for (const f of review.findings) {
      lines.push(`- ${f.file}:${f.line}${f.severity ? ` [${f.severity}]` : ""} ${f.why} (check: ${f.check})`);
    }
  }
  return lines.join("\n");
}
