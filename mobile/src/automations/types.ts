// Wire shapes of the automation.* reads and the one brake (ADR-0044 section 10): what the daemon's automationView and
// automationAuditView carry. Every string here is daemon-provided (names, reasons, notes come from a human or an
// agent) and is only ever drawn as plain <Text>.

export interface AutomationSignal {
  kind: string;
  cron?: string;
  workflow?: string;
  branch?: string;
  conclusions?: string[];
}

export interface AutomationInfo {
  id: number;
  name: string;
  enabled: boolean;
  workspaceId: number;
  signal: AutomationSignal;
  /** 'active' | 'paused'; the engine's brake state, independent of `enabled`. */
  status: string;
  pausedReason: string;
  pausedAt: string | null;
  consecutiveFailures: number;
  budgetBlocked: boolean;
  lastFiredAt: string | null;
  nextRunAt: string | null;
  nextRuns?: string[];
  createdAt: string;
  updatedAt: string;
}

export interface AutomationListing {
  /** The global switch in config.yaml. */
  enabled: boolean;
  /** The global pause in config.yaml. */
  paused: boolean;
  automations: AutomationInfo[];
}

export interface AutomationAuditRow {
  id: number;
  signalKind: string;
  fingerprint: string;
  decision: string;
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

export const AUTOMATION_FIRED_TOPIC = 'automation.fired';
export const AUTOMATION_PAUSED_TOPIC = 'automation.paused';
export const AUTOMATION_UPDATED_TOPIC = 'automation.updated';
/** Every automation event; any of them refetches the screens' reads. */
export const AUTOMATION_TOPICS = [AUTOMATION_FIRED_TOPIC, AUTOMATION_PAUSED_TOPIC, AUTOMATION_UPDATED_TOPIC];
