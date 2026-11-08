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
  /** The pass that raised it and the standards rule it cites (two-axis reviews only, ADR-0054). */
  axis?: string;
  rule?: string;
}

/** One criterion of the spec axis: met, unmet or unverifiable, with the evidence the seat gave. */
export interface StewardCriterion {
  id: string;
  status: string;
  evidence: string;
}

/** One review pass of a two-axis review (ADR-0054 §9); every field is optional on the wire but the axis. */
export interface StewardAxis {
  axis: string;
  status: string;
  seatVerdict?: string;
  summary?: string;
  blockedReason?: string;
  model?: string;
  /** Kept in the type only: cost is shown in the Usage overview, never on the card. */
  costUsd: number;
  runId?: string;
  stewardTaskId?: number;
  criteria?: StewardCriterion[];
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
  axes?: StewardAxis[];
  /** True when the change edits a standards file (ADR-0054); absent otherwise. */
  standardsChanged?: boolean;
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

export type AxisTone = "success" | "warning" | "danger" | "neutral";

/** An axis name as text: the two known axes by name, anything else as its raw text. */
export function axisLabel(axis: string | undefined): string {
  if (axis === "standards") return "Standards";
  if (axis === "spec") return "Spec";
  return axis ? axis : "Axis";
}

/** A pass's chip: its seat verdict once done, otherwise where it stands. Never throws, never empty. */
export function axisStatusLabel(axis: Pick<StewardAxis, "status" | "seatVerdict" | "blockedReason">): { text: string; tone: AxisTone } {
  switch (axis.status) {
    case "done": {
      const v = verdictLabel(axis.seatVerdict);
      return axis.seatVerdict ? v : { text: "Done", tone: "neutral" };
    }
    case "running":
      return { text: "Running", tone: "neutral" };
    case "pending":
      return { text: "Pending", tone: "neutral" };
    case "skipped":
      return { text: axis.blockedReason === "no_spec" ? "Skipped · no spec available" : "Skipped", tone: "neutral" };
    case "blocked":
      return { text: "Did not complete", tone: "danger" };
    default:
      return { text: axis.status ? axis.status : "Pending", tone: "neutral" };
  }
}

/** Why a pass did not complete, as words; an unknown reason renders as its raw text. */
export function blockedReasonLabel(reason: string | undefined): string {
  switch (reason) {
    case "timeout":
      return "Timed out";
    case "budget":
      return "Over its budget";
    case "crashed":
      return "Crashed";
    case "cancelled":
      return "Cancelled";
    case "scope_violation":
      return "Scope violation";
    case "not_run":
      return "Not run";
    case "no_spec":
      return "No spec available";
    default:
      return reason ?? "";
  }
}

/** The spec axis's checklist counts; an unknown status counts as unverifiable. */
export function criteriaCounts(criteria: StewardCriterion[] | undefined): { met: number; unmet: number; unverifiable: number } {
  const out = { met: 0, unmet: 0, unverifiable: 0 };
  for (const c of criteria ?? []) {
    if (c.status === "met") out.met++;
    else if (c.status === "unmet") out.unmet++;
    else out.unverifiable++;
  }
  return out;
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
 * Findings grouped by axis (standards, spec, then any other axis in first-seen order, findings
 * without an axis last), each axis by file as `groupFindingsByFile` does.
 */
export function groupFindingsByAxisThenFile(
  findings: StewardFinding[],
): { axis: string | undefined; files: { file: string; findings: StewardFinding[] }[] }[] {
  const order: (string | undefined)[] = [];
  for (const f of findings) if (!order.includes(f.axis || undefined)) order.push(f.axis || undefined);
  const rank = (a: string | undefined) => (a === "standards" ? 0 : a === "spec" ? 1 : a === undefined ? 3 : 2);
  const sorted = order.map((a, i) => ({ a, i })).sort((x, y) => rank(x.a) - rank(y.a) || x.i - y.i);
  return sorted.map(({ a }) => ({ axis: a, files: groupFindingsByFile(findings.filter((f) => (f.axis || undefined) === a)) }));
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
      const axis = f.axis ? ` [${f.axis}]` : "";
      const rule = f.axis && f.rule ? ` rule: ${f.rule}` : "";
      lines.push(`- ${f.file}:${f.line}${axis}${f.severity ? ` [${f.severity}]` : ""} ${f.why} (check: ${f.check})${rule}`);
    }
  }
  return lines.join("\n");
}
