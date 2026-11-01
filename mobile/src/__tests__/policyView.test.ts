// policyView.test.ts: the policy client sends only the two read RPCs and degrades malformed answers to empty
// (the pluginsReadOnly.test.ts discipline), and the view model stacks the rules and audit rows as plain data.

import { describe, expect, it } from 'vitest';
import { fakeConnection } from './pluginFixtures';
import { fetchPolicy, fetchPolicyAudit, POLICY_READ_METHODS } from '../policy/api';
import { PolicyAuditRow, PolicyStatus } from '../policy/types';
import { auditRows, DASH, policyBanner, policyPath, POLICY_EDIT_NOTE, ruleRows } from '../policy/view';

const NOW = Date.UTC(2026, 0, 15, 12, 0, 0);

function status(over: Partial<PolicyStatus> = {}): PolicyStatus {
  return { enabled: true, valid: true, exists: true, rules: [], ...over };
}

function auditRow(over: Partial<PolicyAuditRow> = {}): PolicyAuditRow {
  return {
    id: 1,
    createdAt: new Date(NOW - 60_000).toISOString(),
    runId: 'run-1',
    taskId: 7,
    requestId: 'perm-1',
    seat: 'lead',
    workspaceId: 3,
    tool: 'Bash',
    target: 'git status',
    paths: [],
    decision: 'ask',
    ruleId: 'git-status',
    policySha: 'abc123',
    reasons: '',
    latencyMs: 4,
    ...over,
  };
}

describe('policy client is read-only', () => {
  it('policy audit read-only', async () => {
    const { conn, calls } = fakeConnection((method) => {
      switch (method) {
        case 'policy.get':
          return { ...status(), rules: 'not an array' };
        case 'policy.audit':
          return { rows: 'not an array' };
        default:
          throw new Error(`unexpected RPC ${method}`);
      }
    });
    const s = await fetchPolicy(conn);
    const rows = await fetchPolicyAudit(conn, 20);
    expect(calls).toEqual([
      ['policy.get', {}],
      ['policy.audit', { limit: 20 }],
    ]);
    expect(calls.every(([m]) => (POLICY_READ_METHODS as readonly string[]).includes(m))).toBe(true);
    expect(s.rules).toEqual([]);
    expect(rows).toEqual([]);
  });
});

describe('policy view model', () => {
  it('policy view stacks rules and audit', () => {
    const rules = ruleRows([
      { id: 'git-status', action: 'ask', why: 'needs a human', seats: ['lead'], tool: 'Bash' },
      { id: 'floor-deny', action: 'deny', why: '' },
    ]);
    expect(rules[0]).toEqual({ id: 'git-status', action: 'ask', why: 'needs a human', scope: 'seats lead, tool Bash' });
    expect(rules[1]).toEqual({ id: 'floor-deny', action: 'deny', why: DASH, scope: DASH });

    const long = 'x'.repeat(200);
    const audit = auditRows([auditRow({ id: 11, target: long, ruleId: '', seat: '' }), auditRow({ id: 12, tool: 'Read', target: '' })], NOW);
    expect(audit.map((r) => r.key)).toEqual(['11', '12']);
    expect(audit[0].target).toBe(`Bash ${long}`.slice(0, 80) + '…');
    expect(audit[0].target.length).toBe(81);
    expect(audit[0].fullTarget).toBe(`Bash ${long}`);
    expect(audit[0].ruleId).toBe(DASH);
    expect(audit[0].seat).toBe(DASH);
    expect(audit[0].time).toBe('1m ago');
    expect(audit[1].target).toBe('Read');
    expect(audit[1].ruleId).toBe('git-status');

    expect(policyPath(status({ path: '/home/.smind/steward/policy.yaml' }))).toBe('/home/.smind/steward/policy.yaml');
    expect(policyPath(status())).toBe(DASH);
    expect(POLICY_EDIT_NOTE).toBe('Edit it on disk; this screen never writes it.');
  });

  it('policy banner', () => {
    expect(policyBanner(status({ valid: false, error: 'yaml: line 3: could not find expected key' }))).toEqual({
      tone: 'danger',
      title: 'Policy file is invalid',
      detail: 'yaml: line 3: could not find expected key',
    });
    expect(policyBanner(status({ valid: false }))).toEqual({ tone: 'danger', title: 'Policy file is invalid', detail: null });
    expect(policyBanner(status({ exists: false }))).toEqual({
      tone: 'neutral',
      title: 'No policy file yet',
      detail: 'Every request that is not floor-denied asks you.',
    });
    expect(policyBanner(status({ enabled: true }))).toEqual({ tone: 'success', title: 'Policy active', detail: null });
    expect(policyBanner(status({ enabled: false }))).toEqual({
      tone: 'neutral',
      title: 'Policy file is valid but the engine is off (steward.policy.enabled)',
      detail: null,
    });
  });
});
