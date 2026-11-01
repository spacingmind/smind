// api.ts is the policy client surface of the mobile app: two READ calls and nothing else. The policy file is
// human-edited on disk; this screen never writes it, and policy.get/policy.audit are the only RPCs it sends.

import { RelayConnection } from '../relay/RelayConnection';
import { PolicyAuditRow, PolicyStatus } from './types';

/** Every RPC this module may send. */
export const POLICY_READ_METHODS = ['policy.get', 'policy.audit'] as const;

export async function fetchPolicy(conn: RelayConnection): Promise<PolicyStatus> {
  const res = (await conn.call('policy.get', {})) as Partial<PolicyStatus> | null;
  return { ...res, rules: Array.isArray(res?.rules) ? res.rules : [] };
}

export async function fetchPolicyAudit(conn: RelayConnection, limit = 50): Promise<PolicyAuditRow[]> {
  const res = (await conn.call('policy.audit', { limit })) as { rows?: PolicyAuditRow[] } | null;
  return Array.isArray(res?.rows) ? res.rows : [];
}
