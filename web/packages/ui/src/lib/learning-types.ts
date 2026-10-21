/**
 * The wire contract of the `learning.*` RPCs and `learnedSkill.*` / `learning.job`
 * events (ADR-0026 §8, internal/wsapi). JSON is camelCase; every id is the
 * store's int64 as a number. Nothing here is derived from another module: the
 * daemon owns these shapes and this file mirrors them.
 */

export type LearningMode = "off" | "propose";
export type LearnedSkillStatus = "active" | "disabled" | "archived";
export type LearnedSkillSource = "reflector" | "curator" | "human";

export interface LearnedSkillView {
  id: number;
  workspaceId: number;
  name: string;
  category: string;
  description: string;
  /** Only present when the list/get asked for `includeBody`. */
  body?: string;
  status: LearnedSkillStatus;
  pinned: boolean;
  source: LearnedSkillSource;
  /** Run ids are the run registry's text ids (`runs.id`), not numbers. */
  createdByRun: string;
  /** Name of a live skill this one resembles; "" when none. */
  possibleDuplicateOf: string;
  similarity: number;
  version: number;
  offeredRuns: number;
  loadedRuns: number;
  loads: number;
  loadsViaSkill: number;
  loadsViaRead: number;
  patched: boolean;
  reusedAfterPatch: boolean;
  /** False until the skill has been offered enough to measure a load rate. */
  measured: boolean;
  probation: boolean;
  loadRate: number;
  lastOfferedAt?: string;
  lastLoadedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export type ProposalKind = "create" | "patch" | "bundle";
export type ProposalStatus = "pending" | "approved" | "rejected";
export type ProposalItemAction = "create" | "patch" | "delete";
export type EvidenceKind = "user_message" | "failure_recovery" | "permission_denied" | "loaded_skill_wrong";

export interface ProposalItem {
  action: ProposalItemAction;
  name: string;
  category: string;
  description: string;
  body: string;
  oldString: string;
  newString: string;
  absorbedInto: string;
  reason: string;
  targetSkillId?: number;
  baseVersion?: number;
}

export interface ProposalEvidence {
  kind: EvidenceKind;
  runId: string;
  /** Event seq in the run log; -1 is the run's prompt record. */
  seq: number;
  quote: string;
}

export interface ProposalView {
  id: number;
  workspaceId: number;
  kind: ProposalKind;
  source: LearnedSkillSource;
  name: string;
  status: ProposalStatus;
  note: string;
  jobId?: number;
  possibleDuplicateOf: string;
  similarity: number;
  createdAt: string;
  decidedAt?: string;
  malformed?: boolean;
  items: ProposalItem[];
  evidence: ProposalEvidence[];
}

export interface VersionView {
  version: number;
  description: string;
  body: string;
  actor: string;
  createdAt: string;
}

export interface LedgerEntryView {
  id: number;
  skillId?: number;
  skillName: string;
  action: string;
  reason: string;
  absorbedInto: string;
  runId: string;
  actor: string;
  createdAt: string;
}

export interface JobView {
  id: number;
  workspaceId: number;
  chatId?: number;
  kind: string;
  trigger: string;
  status: string;
  skipReason: string;
  error: string;
  costUsd?: number;
  tokens?: number;
  createdAt: string;
  finishedAt?: string;
}

/** A workspace's reflection spend in the daemon's rolling window; a cap of 0 means no limit. */
export interface LearningWorkspaceBudget {
  windowHours: number;
  /** Reported cost only. */
  usedUsd: number;
  /** Includes an estimate for runs that reported no usage (`unreportedRuns`). */
  usedTokens: number;
  capUsd: number;
  capTokens: number;
  unreportedRuns: number;
}

export interface LearningWorkspaceStatus {
  id: number;
  title: string;
  learningMode: LearningMode;
  pendingProposals: number;
  activeSkills: number;
  disabledSkills: number;
  /** Absent from daemons that predate the budget. */
  budget?: LearningWorkspaceBudget;
}

export interface LearningStatus {
  enabled: boolean;
  workspaces: LearningWorkspaceStatus[];
  budget: Record<string, unknown> | null;
}

export interface LearnedSkillDetail {
  skill: LearnedSkillView;
  versions: VersionView[];
  ledger: LedgerEntryView[];
  /** The quotes the skill was learned from. Not in the agreed contract yet; shown when the daemon sends it. */
  evidence?: ProposalEvidence[];
}

export interface ProposalDetail {
  proposal: ProposalView;
  /** Present iff a pending create's possibleDuplicateOf is a LEARNED live skill (target carries its body). */
  merge?: { target: LearnedSkillView; similarity: number };
}

/** One failure/permission signature of a proposal and how often it was seen in the workspace (ADR-0037 S0). */
export interface ProposalPrevalenceSignature {
  kind: "failure_recovery" | "permission_denied";
  tool: string;
  label: string;
  runs: number;
  chats: number;
  lastSeenAt?: string;
  truncated: boolean;
}

export interface ProposalPrevalence {
  windowDays: number;
  signatures: ProposalPrevalenceSignature[];
}

export type ExportTarget = "repo" | "claude" | "agents" | "codex";

export interface ExportSkillParams {
  id: number;
  target: ExportTarget;
  /** Replace a SKILL.md smind found edited or unmanaged; only sent after a `modified` result. */
  force?: boolean;
}

export interface ExportResult {
  dir: string;
  /** Files written; empty for an idempotent re-export. */
  files: string[];
  /** "installed" | "modified" | "outdated". */
  status: string;
  /** Files smind left alone because they differ from what it would write. */
  skipped: string[];
}

export interface ApproveProposalParams {
  id: number;
  description?: string;
  body?: string;
  /** Name of the LEARNED skill to merge the (create) proposal into; the daemon identifies the target by name. */
  mergeInto?: string;
}

/** `data.findings` of a `gate_rejected` error. */
export interface GateFinding {
  reason: string;
  detail: string;
}

/** Event payloads. */
export interface LearnedSkillProposedPayload {
  workspaceId: number;
  proposalId: number;
  name: string;
  kind: ProposalKind;
}
export interface LearnedSkillChangedPayload {
  workspaceId: number;
  skillId?: number;
  proposalId?: number;
  reason: string;
}
export interface LearningJobPayload {
  workspaceId: number;
  jobId: number;
  status: string;
}
