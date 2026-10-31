// hostsRoute.test.ts: pure checks on the Hosts route and screen sources (no rendering). The screen must only
// reach the daemon through hosts/api, must not name pairing or grant-editing methods, and must never prefill
// the code the human is supposed to type.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { HOSTS_HREF } from '../navigation/routes';

const ROOT = join(__dirname, '..', '..');
const route = readFileSync(join(ROOT, 'app', 'hosts.tsx'), 'utf8');
const screen = readFileSync(join(ROOT, 'src', 'screens', 'HostsScreen.tsx'), 'utf8');

const FORBIDDEN = [
  'fleet.host.grant',
  'fleet.host.invite',
  'fleet.host.add',
  'fleet.host.rm',
  'fleet.host.enable_reverse',
  'fleet.host.disable_reverse',
  'fleet.host.revoke',
  'fleet.host.rename',
];

describe('hosts route', () => {
  it('lives at /hosts', () => {
    expect(HOSTS_HREF).toBe('/hosts');
  });

  it('the screen uses the hosts client and names no forbidden method', () => {
    expect(screen).toMatch(/from '\.\.\/hosts\/api'/);
    for (const src of [route, screen]) {
      for (const m of FORBIDDEN) expect(src).not.toContain(m);
    }
  });

  it('the route only hands the screen a string focus', () => {
    expect(route).toContain("typeof params.focus === 'string'");
  });

  it('the code field disables autofill and is never prefilled with the host code', () => {
    expect(screen).toContain('autoComplete="off"');
    expect(screen).toContain('textContentType="none"');
    expect(screen).not.toMatch(/useState\((host|h)\.sas|setTyped\((host|h)\.sas/);
  });
});
