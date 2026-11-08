// types.ts is the wire shape of `steward.get` (the steward review card): the daemon computes every
// gate and clamps the verdict, the card only renders. Every string is daemon-provided text and is
// only ever drawn through a <Text>, never interpreted or executed by the phone.

/** One finding of a CHANGES review: where, why, and the exact check that would show it fixed. */
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

/** One review pass of a two-axis review (ADR-0054 section 9); every field but the axis may be missing on the wire. */
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

/** One objective gate, each pass | fail | unknown | n/a. */
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

/** `steward.get`'s answer: commit-bound and stale-aware; the phone never decides readiness from it. */
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
  /** Two-axis reviews only (ADR-0054); absent for a single-axis review. */
  axes?: StewardAxis[];
  /** True for a one-axis re-run: the other pass was not run, so it can never be accepted. */
  partial?: boolean;
  /** The standards file was edited in this change. */
  standardsChanged?: boolean;
  prNumber?: number;
  stale?: string;
  notReadyReason?: string;
  scopeViolation?: boolean;
  costUsd: number;
  createdAt: string;
  finishedAt?: string;
}
