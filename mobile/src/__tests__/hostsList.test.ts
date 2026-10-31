// hostsList.test.ts: the fleet.host.list read (including the fleet-off mapping) and the Hosts screen's pure
// view: link labels/tones/details and the off/empty/list states.

import { describe, expect, it } from 'vitest';
import { fetchHosts } from '../hosts/api';
import { FleetLinkState } from '../hosts/types';
import { formatSas, hostRowView, hostsView, linkLabel } from '../hosts/view';
import { fakeConnection, host } from './hostsFixtures';

describe('fetchHosts', () => {
  it('returns the daemon list', async () => {
    const { conn, calls } = fakeConnection(() => ({ hosts: [host()] }));
    expect(await fetchHosts(conn)).toEqual({ off: false, hosts: [host()] });
    expect(calls).toEqual([['fleet.host.list', {}]]);
  });

  it('maps method not found to fleet off', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('method not found: fleet.host.list');
    });
    expect(await fetchHosts(conn)).toEqual({ off: true, hosts: [] });
  });

  it('rethrows every other error and degrades a malformed answer to no hosts', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('relay closed');
    });
    await expect(fetchHosts(conn)).rejects.toThrow('relay closed');
    const { conn: conn2 } = fakeConnection(() => null);
    expect(await fetchHosts(conn2)).toEqual({ off: false, hosts: [] });
  });
});

describe('hostsView', () => {
  it('fleet off, nothing paired, and a list of rows', () => {
    expect(hostsView({ off: true, hosts: [host()] })).toEqual({
      kind: 'off',
      title: 'Fleet is off',
      text: 'Turn fleet on in config.yaml on the computer running smind.',
    });
    expect(hostsView({ off: false, hosts: [] })).toEqual({
      kind: 'empty',
      title: 'No hosts paired',
      text: 'Pair hosts from Settings -> Hosts on a computer.',
    });
    const v = hostsView({ off: false, hosts: [host({ hostId: 'h2', alias: 'win' }), host()] });
    expect(v.kind).toBe('list');
    if (v.kind !== 'list') return;
    expect(v.rows.map((r) => r.key)).toEqual(['h2', 'h1']);
    expect(v.rows[0]).toMatchObject({ alias: 'win', label: 'Mac', status: 'Connected', tone: 'ok', detail: null, canApprove: false });
  });
});

describe('hostRowView', () => {
  it('labels every link state like the web Hosts page', () => {
    expect(linkLabel('active')).toBe('Connected');
    expect(linkLabel('connecting')).toBe('Connecting');
    expect(linkLabel('down')).toBe('Unreachable');
    expect(linkLabel('refused')).toBe('Refused');
    expect(linkLabel('peer_disabled')).toBe('Peer turned this direction off');
    expect(linkLabel('disabled')).toBe('Off');
    expect(linkLabel('revoked')).toBe('Revoked');
  });

  it('tones: connected ok, connecting/down warning, refused/revoked danger, the rest neutral', () => {
    const tone = (link: FleetLinkState) => hostRowView(host({ link })).tone;
    expect(tone('active')).toBe('ok');
    expect(tone('connecting')).toBe('warning');
    expect(tone('down')).toBe('warning');
    expect(tone('refused')).toBe('danger');
    expect(tone('revoked')).toBe('danger');
    expect(tone('peer_disabled')).toBe('neutral');
    expect(tone('disabled')).toBe('neutral');
  });

  it('a refusal shows the guidance, falling back to the code', () => {
    expect(hostRowView(host({ link: 'refused', linkCode: 'fleet_version_unsupported', linkGuidance: 'Update smind on mac (has 0.9, needs >= 1.0)' })))
      .toMatchObject({ status: 'Refused', detail: 'Update smind on mac (has 0.9, needs >= 1.0)' });
    expect(hostRowView(host({ link: 'refused', linkCode: 'peer_identity_changed' }))).toMatchObject({ detail: 'peer_identity_changed' });
    expect(hostRowView(host({ link: 'refused' })).detail).toBeNull();
  });

  it('the two pending states', () => {
    expect(hostRowView(host({ state: 'pending' }))).toMatchObject({ detail: 'Waiting for approval on mac', canApprove: false });
    expect(hostRowView(host({ state: 'pending', pendingApproval: true, sas: '123456' }))).toMatchObject({ detail: 'Awaiting your approval', canApprove: true });
    expect(hostRowView(host({ state: 'pending', pendingApproval: true })).canApprove).toBe(false);
  });
});

describe('sas helpers', () => {
  it('formats six digits with a space', () => {
    expect(formatSas('123456')).toBe('123 456');
    expect(formatSas('123 456')).toBe('123 456');
    expect(formatSas('12345')).toBe('12345');
  });
});
