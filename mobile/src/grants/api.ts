// api.ts: the saved-grant client surface. Three calls: the list, the revoke, and the scoped answer a permission
// card sends. All three are human-only on the daemon (no MCP tool, no agent connection reaches them).

import { RelayConnection } from '../relay/RelayConnection';
import { SavedGrant } from './types';

/** Every RPC this module may send. */
export const GRANT_METHODS = ['permission.grants.list', 'permission.grants.revoke', 'run.answerPermission'] as const;

export async function fetchGrants(conn: RelayConnection): Promise<SavedGrant[]> {
  const res = (await conn.call('permission.grants.list', {})) as { grants?: SavedGrant[] } | null;
  return Array.isArray(res?.grants) ? res.grants : [];
}

export async function revokeGrant(conn: RelayConnection, grantId: number): Promise<void> {
  await conn.call('permission.grants.revoke', { grantId });
}

/**
 * Allow this request with its single-allow option AND save it as a project grant. allowProjectCode is the explicit
 * opt-in a command that runs project code needs (the daemon decides whether that is so, and refuses otherwise,
 * leaving the request pending).
 */
export async function answerPermissionAlways(
  conn: RelayConnection,
  runId: string,
  requestId: string,
  optionId: string,
  allowProjectCode: boolean,
): Promise<void> {
  const params: Record<string, unknown> = { runId, requestId, optionId, scope: 'project' };
  if (allowProjectCode) params.allowProjectCode = true;
  await conn.call('run.answerPermission', params);
}
