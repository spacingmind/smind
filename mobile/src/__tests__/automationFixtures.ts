// Shared fixtures for the automation tests: wire-shaped automation.* payloads (see pluginFixtures.ts for the
// scripted connection).

import { AutomationAuditRow, AutomationInfo, AutomationListing } from '../automations/types';

export { fakeConnection } from './pluginFixtures';

export function automation(over: Partial<AutomationInfo> = {}): AutomationInfo {
  return {
    id: 1,
    name: 'Nightly triage',
    enabled: true,
    workspaceId: 1,
    signal: { kind: 'cron', cron: '17 19 * * *' },
    status: 'active',
    pausedReason: '',
    pausedAt: null,
    consecutiveFailures: 0,
    budgetBlocked: false,
    lastFiredAt: null,
    nextRunAt: null,
    createdAt: '2026-10-20T10:00:00Z',
    updatedAt: '2026-10-20T10:00:00Z',
    ...over,
  };
}

export function listing(automations: AutomationInfo[], enabled = true, paused = false): AutomationListing {
  return { enabled, paused, automations };
}

export function auditRow(over: Partial<AutomationAuditRow> = {}): AutomationAuditRow {
  return {
    id: 1,
    signalKind: 'cron',
    fingerprint: 'fp',
    decision: 'fire',
    reason: '',
    manual: false,
    catchup: false,
    taskId: null,
    runId: '',
    outcome: '',
    note: '',
    createdAt: '2026-10-21T19:17:00Z',
    finishedAt: null,
    ...over,
  };
}
