// automationsReadOnly.test.ts: the phone never mutates an automation except by the Pause brake. Walks the api module
// (calls made) and every automation source and screen (strings named).

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { AUTOMATION_METHODS, disableAutomation, fetchAutomation, fetchAutomationAudit, fetchAutomations } from '../automations/api';
import { pauseWithConfirm } from '../automations/pause';
import { automation, auditRow, fakeConnection, listing } from './automationFixtures';

const SRC = join(__dirname, '..');
const ROOT = join(SRC, '..');
const ALLOWED = ['automation.list', 'automation.get', 'automation.audit', 'automation.disable'];

function files(dir: string): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir)) {
    const full = join(dir, e);
    if (statSync(full).isDirectory()) {
      if (e !== '__tests__' && e !== 'node_modules') out.push(...files(full));
    } else if (/\.(ts|tsx)$/.test(e)) out.push(full);
  }
  return out;
}

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
/** Every automation module, screen and route: anything under automation(s)/, named automation*, or the two routes. */
const AUTOMATION_FILES = [...files(SRC), ...files(join(ROOT, 'app'))].filter((f) => /[\\/]automations?[\\/]|[\\/]automation[A-Za-z]*\.(ts|tsx)$|[\\/]Automation[A-Za-z]*\.(ts|tsx)$/i.test(f));

describe('automation client is read-only', () => {
  it('the exported method list is exactly the allowlist', () => {
    expect([...AUTOMATION_METHODS].sort()).toEqual([...ALLOWED].sort());
  });

  it('every flow sends only list / get / audit / disable', async () => {
    const { conn, calls } = fakeConnection((m) => (m === 'automation.list' ? listing([automation()]) : m === 'automation.audit' ? { rows: [auditRow()] } : m === 'automation.get' ? automation() : {}));
    await fetchAutomations(conn);
    await fetchAutomation(conn, 1);
    await fetchAutomationAudit(conn, 1);
    await disableAutomation(conn, 1);
    await pauseWithConfirm({ name: 'x', confirm: async () => true, disable: () => disableAutomation(conn, 1) });
    expect(calls.length).toBe(5);
    expect(calls.every(([m]) => ALLOWED.includes(m))).toBe(true);
  });

  it('the walk finds the automation files (it would pass vacuously otherwise)', () => {
    const names = AUTOMATION_FILES.map((f) => f.slice(ROOT.length + 1));
    for (const want of ['src/automations/api.ts', 'src/screens/AutomationsScreen.tsx', 'src/screens/AutomationDetailScreen.tsx', 'app/automations.tsx']) expect(names).toContain(want);
  });

  it('every RPC name in an automation file is on the allowlist, and no mutating word appears', () => {
    const rpc = /['"`](automation\.[A-Za-z.]+)['"`]/g;
    const mutating = /automation\.(create|update|enable|resume|fire|delete)\b|memory\.clear|automation\.memory/;
    for (const f of AUTOMATION_FILES) {
      const src = strip(readFileSync(f, 'utf8'));
      for (const m of src.matchAll(rpc)) {
        // Event topics (automation.fired / paused / updated) are not RPCs.
        if (['automation.fired', 'automation.paused', 'automation.updated'].includes(m[1])) continue;
        expect(ALLOWED, `${f}: ${m[1]}`).toContain(m[1]);
      }
      expect(src, f).not.toMatch(mutating);
    }
  });

  it('no screen or route calls the connection directly: only through api.ts', () => {
    for (const f of AUTOMATION_FILES.filter((x) => /Screen|app[\\/]/.test(x) || /screens/.test(x))) {
      expect(strip(readFileSync(f, 'utf8')), f).not.toMatch(/\.call\s*\(/);
    }
    const apiFiles = AUTOMATION_FILES.filter((f) => /\.call\s*\(/.test(strip(readFileSync(f, 'utf8')))).map((f) => f.slice(SRC.length + 1));
    expect(apiFiles).toEqual(['automations/api.ts']);
  });
});
