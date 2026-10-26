// The automation.* wire (internal/wsapi/automations.go automationView / automationAuditView, internal/automation
// Def/Template/Rails/SignalSpec; ADR-0044). Reads are open to every connection; every mutation except `disable` is
// human + local only and may answer `forbidden`.

export type AutomationSignalKind = "cron" | "github.workflow_run";

export interface AutomationSignalSpec {
  kind: AutomationSignalKind;
  cron?: string;
  workflow?: string;
  branch?: string;
  conclusions?: string[];
}

export interface AutomationTemplate {
  profileId: number;
  model: string;
  mode: string;
  autoAccept?: boolean;
  titleTemplate: string;
  promptTemplate: string;
  budgetUsd: number;
  maxRunMinutes: number;
  baseRef?: string;
}

export interface AutomationRails {
  maxRunsPerHour: number;
  dedupeWindowMinutes?: number;
  pauseAfterFailures: number;
  memoryEnabled: boolean;
  memoryMaxBytes: number;
  pollMinutes?: number;
}

/** The human-authored part: what `automation.create` takes and `automation.update` takes beside `id`. */
export interface AutomationDef {
  name: string;
  workspaceId: number;
  signal: AutomationSignalSpec;
  template: AutomationTemplate;
  rails: AutomationRails;
}

export type AutomationStatus = "active" | "paused" | string;

export interface AutomationView extends AutomationDef {
  id: number;
  enabled: boolean;
  status: AutomationStatus;
  pausedReason: string;
  pausedAt: string | null;
  consecutiveFailures: number;
  /** The last run reached its budget: the next fire is held. */
  budgetBlocked: boolean;
  lastFiredAt: string | null;
  nextRunAt: string | null;
  /** The next three fire times (RFC3339, UTC) of a cron signal. */
  nextRuns?: string[];
  memoryEnabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface AutomationListing {
  /** The daemon switch (`automations.enabled` in config.yaml): a file edit only, no RPC. */
  enabled: boolean;
  /** The global pause (`automations.paused`). */
  paused: boolean;
  automations: AutomationView[];
}

export type AutomationDecision = "fire" | "deduped" | "capped" | "paused" | "missed" | "failed" | string;

export interface AutomationAuditRow {
  id: number;
  signalKind: string;
  fingerprint: string;
  decision: AutomationDecision;
  reason: string;
  manual: boolean;
  catchup: boolean;
  taskId: number | null;
  runId: string;
  outcome: string;
  note: string;
  createdAt: string;
  finishedAt: string | null;
}

export interface AutomationAuditFilter {
  /** Empty = every decision. Filtering is client-side on the loaded pages. */
  decision?: string;
}

/** `automation.fired` payload (ids and a short decision only, never signal text). */
export interface AutomationFiredPayload {
  automationId: number;
  auditId: number;
  taskId?: number;
  decision: AutomationDecision;
  fingerprint: string;
}

export interface AutomationPausedPayload {
  automationId: number;
  reason: string;
}

export interface AutomationUpdatedPayload {
  automationId: number;
}

export const AUTOMATION_PAGE_SIZE = 25;
