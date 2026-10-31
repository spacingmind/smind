// hostsApprove.test.ts: the one write a phone may do. Approval always carries the fixed read-only grant and
// the typed SAS (spaces dropped); the Approve button only lights up when the typed code is exactly the six
// digits the daemon sent for this pending host.

import { describe, expect, it } from 'vitest';
import { approveHost, READ_ONLY_GRANT, rejectHost } from '../hosts/api';
import { approveEnabled } from '../hosts/view';
import { fakeConnection, host } from './hostsFixtures';

const pending = host({ state: 'pending', pendingApproval: true, sas: '123456' });

describe('approve and reject calls', () => {
  it('approve sends exactly the host id, the digits-only SAS and the read-only grant', async () => {
    const { conn, calls } = fakeConnection(() => ({ host: host({ state: 'approved', pendingApproval: false }) }));
    await approveHost(conn, 'h1', '123 456');
    expect(calls).toEqual([['fleet.host.approve', { hostId: 'h1', sas: '123456', grant: READ_ONLY_GRANT }]]);
  });

  it('the grant a phone can send is read only: no write op, no writable workspace, no agents, no time, no cost', () => {
    expect(READ_ONLY_GRANT.ops).toEqual(['read']);
    expect(READ_ONLY_GRANT.workspacesWrite).toEqual([]);
    expect(READ_ONLY_GRANT.maxLiveAgents).toBe(0);
    expect(READ_ONLY_GRANT.maxWallSeconds).toBe(0);
    expect(READ_ONLY_GRANT.maxCostUsd).toBe(0);
  });

  it('reject sends only the host id', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    await rejectHost(conn, 'h1');
    expect(calls).toEqual([['fleet.host.reject', { hostId: 'h1' }]]);
  });
});

describe('approveEnabled', () => {
  it('is false for empty, short, wrong and too-long codes', () => {
    expect(approveEnabled(pending, '')).toBe(false);
    expect(approveEnabled(pending, '123')).toBe(false);
    expect(approveEnabled(pending, '654321')).toBe(false);
    expect(approveEnabled(pending, '1234567')).toBe(false);
  });

  it('is false when the host has no SAS or does not wait on this human', () => {
    expect(approveEnabled(host({ state: 'pending', pendingApproval: true }), '123456')).toBe(false);
    expect(approveEnabled(host({ state: 'approved', sas: '123456' }), '123456')).toBe(false);
  });

  it('is true when the typed code, spaces aside, is the daemon-provided SAS', () => {
    expect(approveEnabled(pending, '123 456')).toBe(true);
    expect(approveEnabled(pending, '123456')).toBe(true);
  });
});
