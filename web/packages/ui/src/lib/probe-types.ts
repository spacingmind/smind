/**
 * Probe-before-approve wire contract (ADR-0037 §8, docs/plans/active/learning-probe.md, "Probe wire
 * contract", frozen 2026-10-22). JSON is camelCase, money is USD, durations are ms, times RFC 3339.
 * `id` in every `learning.probe.*` param is the proposal id.
 */

export type ProbeReasonCode =
  | "not_pending"
  | "provider_not_claude_native"
  | "no_prompt"
  | "no_base_sha"
  | "dirty_checkout"
  | "continuation_run"
  | "source_cost_unknown"
  | "sandbox_unavailable"
  | "budget"
  | "quota"
  | "busy"
  | "windows"
  | "platform_unsupported"
  | "learning_off";

/** `message` is user-facing, one sentence; the UI shows it verbatim and never invents reasons. */
export interface ProbeReason {
  code: ProbeReasonCode;
  message: string;
}

export interface ProbeEstimate {
  eligible: boolean;
  reasons: ProbeReason[]; // empty iff eligible
  quick: boolean;
  arms: 2 | 3;
  estUSD: number;
  estArmUSD: number;
  expectedMinutes: number;
  perProbeCapUSD: number;
  dailyCapUSD: number;
  spentTodayUSD: number;
  quotaPercent: number | null;
}

export type ProbeStatus = "queued" | "running" | "done" | "failed" | "cancelled" | "unsafe";
export type ProbeVerdict = "helped" | "no_effect" | "never_loaded" | "hurt" | "inconclusive" | "probe_failed" | "unsafe";
/** Control / Control replicate / With this skill. */
export type ProbeArmRole = "A" | "A2" | "B";
export type ProbeArmStatus = "pending" | "running" | "done" | "capped" | "timeout" | "turns" | "output" | "killed" | "error";

export interface ProbeArmView {
  role: ProbeArmRole;
  status: ProbeArmStatus;
  runId?: string; // hidden run; link target
  startedAt?: string;
  elapsedMs: number;
  costUSD: number;
  turns: number;
  toolCalls: number;
  failedToolCalls: number;
  permissionDenials: number;
  loadedSkill?: boolean; // role B only
  failureRecurred?: boolean | null; // null = evidence has no signature
  producedDiff: boolean;
  diffBytes: number;
  diff?: string; // get only; redacted; <= 64 KB
  finalMessage?: string; // get only; redacted; <= 8 KB
  check?: { passed: boolean; output?: string }; // present only when probeCheck is configured
}

export type ProbeMetricKey =
  | "toolCalls"
  | "failedToolCalls"
  | "permissionDenials"
  | "turns"
  | "costUSD"
  | "durationMs"
  | "diffBytes";

/** `worse` = B worse than A by more than the noise floor. */
export interface ProbeMetricRow {
  key: ProbeMetricKey;
  a: number;
  a2: number | null;
  b: number;
  deltaAB: number;
  noise: number | null;
  worse: boolean;
}

export interface ProbeView {
  id: number;
  proposalId: number;
  workspaceId: number;
  quick: boolean;
  status: ProbeStatus;
  verdict: ProbeVerdict | null;
  verdictReason: string; // one sentence, advisory
  estUSD: number;
  costUSD: number;
  startedAt: string;
  finishedAt?: string;
  elapsedMs: number;
  arms: ProbeArmView[]; // planned arms in order, pending ones included
  contrast?: { loaded: boolean; noiseFloor: boolean; metrics: ProbeMetricRow[] }; // done probes only
}

/** `learnedSkill.probe.{started,arm,finished,failed}`: a hint; the UI refetches. */
export interface LearnedSkillProbePayload {
  probeId: number;
  proposalId: number;
  workspaceId: number;
  status: ProbeStatus;
  role?: ProbeArmRole;
  armStatus?: ProbeArmStatus;
}
