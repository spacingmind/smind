// grantsSafety.test.ts: the saved-grant client sends only its three calls, never opens a link or renders markdown,
// and never lets a caller assert the class (the offer's class is only ever displayed; the answer carries no class).

import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { GRANT_METHODS, answerPermissionAlways, fetchGrants, revokeGrant } from '../grants/api';
import { fakeConnection } from './pluginFixtures';

const ROOT = join(__dirname, '..', '..');
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
const SOURCES = [...readdirSync(join(ROOT, 'src', 'grants')).map((f) => `src/grants/${f}`), 'src/screens/SavedGrantsScreen.tsx', 'src/screens/task/GrantControl.tsx', 'src/ui/OutlineButton.tsx'].map((f) => ({
  f,
  text: strip(readFileSync(join(ROOT, f), 'utf8')),
}));

describe('saved grants client', () => {
  it('sends only list, revoke and the scoped answer, with exactly these params', async () => {
    const { conn, calls } = fakeConnection((m) => (m === 'permission.grants.list' ? { grants: [] } : {}));
    await fetchGrants(conn);
    await revokeGrant(conn, 4);
    await answerPermissionAlways(conn, 'r', 'q', 'allow', true);
    await answerPermissionAlways(conn, 'r', 'q', 'allow', false);
    expect(calls).toEqual([
      ['permission.grants.list', {}],
      ['permission.grants.revoke', { grantId: 4 }],
      ['run.answerPermission', { runId: 'r', requestId: 'q', optionId: 'allow', scope: 'project', allowProjectCode: true }],
      ['run.answerPermission', { runId: 'r', requestId: 'q', optionId: 'allow', scope: 'project' }],
    ]);
    expect(calls.every(([m]) => (GRANT_METHODS as readonly string[]).includes(m))).toBe(true);
  });

  it('a malformed list answer degrades to empty', async () => {
    const { conn } = fakeConnection(() => ({ grants: 'nope' }));
    expect(await fetchGrants(conn)).toEqual([]);
  });

  it('no request this client builds carries a class (the daemon decides it)', () => {
    const api = SOURCES.find((s) => s.f.endsWith('grants/api.ts'))!.text;
    expect(api).not.toMatch(/\bclass\b/);
  });

  it('no source opens a link or renders markdown', () => {
    for (const { f, text } of SOURCES) {
      expect(text, f).not.toMatch(/Linking|openURL|dataDetector|autolink/i);
      expect(text, f).not.toMatch(/Markdown|markdown/);
    }
  });
});
