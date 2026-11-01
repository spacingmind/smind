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
  prNumber?: number;
  stale?: string;
  notReadyReason?: string;
  scopeViolation?: boolean;
  costUsd: number;
  createdAt: string;
  finishedAt?: string;
}
