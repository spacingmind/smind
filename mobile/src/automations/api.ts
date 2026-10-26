// api.ts is the whole automation client surface of the mobile app: three READ calls and the one brake
// (automation.disable, which the daemon allows over the relay). create/update/enable/resume/fire/delete and the memory
// calls are human + local only (ADR-0044 section 10); the phone does not even offer them. automationsReadOnly.test.ts
// pins that no other method name is ever sent.

import { RelayConnection } from '../relay/RelayConnection';
import { AutomationAuditRow, AutomationInfo, AutomationListing } from './types';

/** Every RPC this module may send. */
export const AUTOMATION_METHODS = ['automation.list', 'automation.get', 'automation.audit', 'automation.disable'] as const;

export const AUDIT_PAGE_SIZE = 25;

export async function fetchAutomations(conn: RelayConnection): Promise<AutomationListing> {
  const res = (await conn.call('automation.list')) as Partial<AutomationListing> | null;
  return {
    enabled: res?.enabled === true,
    paused: res?.paused === true,
    automations: Array.isArray(res?.automations) ? res.automations : [],
  };
}

export async function fetchAutomation(conn: RelayConnection, id: number): Promise<AutomationInfo> {
  return (await conn.call('automation.get', { id })) as AutomationInfo;
}

/** One page of run history, newest first; `before` is the id of the oldest row already shown (0 = from the top). */
export async function fetchAutomationAudit(conn: RelayConnection, id: number, before = 0, limit = AUDIT_PAGE_SIZE): Promise<AutomationAuditRow[]> {
  const res = (await conn.call('automation.audit', { id, limit, before })) as { rows?: AutomationAuditRow[] } | null;
  return Array.isArray(res?.rows) ? res.rows : [];
}

/** The brake: switches the automation off. The daemon's error text, if any, is the rejection's message. */
export async function disableAutomation(conn: RelayConnection, id: number): Promise<void> {
  await conn.call('automation.disable', { id });
}
