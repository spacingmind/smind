// hostsSafety.test.ts: mobile only ever lists hosts and approves/rejects a pending one (ADR-0048 S13). The
// sources are scanned, not just the calls made, so a future screen cannot grow pairing or grant editing.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { HOST_METHODS } from '../hosts/api';
import { approveEnabled } from '../hosts/view';
import { host } from './hostsFixtures';

const SRC = join(__dirname, '..');
const ROOT = join(SRC, '..');

function sources(dir: string): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir)) {
    const full = join(dir, e);
    if (statSync(full).isDirectory()) {
      if (e !== '__tests__') out.push(...sources(full));
    } else if (/\.(ts|tsx)$/.test(e)) out.push(full);
  }
  return out;
}

const strip = (f: string) => readFileSync(f, 'utf8').replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, '');

/** Pairing, grant editing, removal and reverse toggles are human + local-only; a phone never names them. */
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

describe('the hosts client stays a viewer plus one approval', () => {
  it('every fleet.host method named under src/hosts is in HOST_METHODS', () => {
    const allowed = new Set<string>(HOST_METHODS);
    const hits: string[] = [];
    for (const f of sources(join(SRC, 'hosts'))) {
      for (const m of strip(f).match(/fleet\.host\.[a-z_]+/g) ?? []) {
        if (!allowed.has(m)) hits.push(`${f.slice(SRC.length + 1)}: ${m}`);
      }
    }
    expect(hits).toEqual([]);
  });

  it('no mobile source names a pairing, grant, removal or reverse method', () => {
    const hits = [...sources(SRC), ...sources(join(ROOT, 'app'))].filter((f) => FORBIDDEN.some((m) => strip(f).includes(m)));
    expect(hits).toEqual([]);
  });

  it('the view never fills a SAS: an empty typed code never approves', () => {
    expect(approveEnabled(host({ state: 'pending', pendingApproval: true, sas: '123456' }), '')).toBe(false);
  });
});
