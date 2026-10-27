// inboxSafety.test.ts: the inbox code has no approve-all / merge / always-allow, never sends snooze/resolve with a
// perm: key, and renders daemon text only as plain <Text>.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { inboxView } from '../inbox/view';
import { inboxItem } from './inboxFixtures';

const ROOT = join(__dirname, '..', '..');
function files(dir: string): string[] {
  return readdirSync(dir).flatMap((e) => {
    const full = join(dir, e);
    return statSync(full).isDirectory() ? files(full) : /\.(ts|tsx)$/.test(e) ? [full] : [];
  });
}
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
const SOURCES = [
  ...files(join(ROOT, 'src', 'inbox')),
  ...files(join(ROOT, 'src', 'screens', 'inbox')),
  join(ROOT, 'src/screens/InboxScreen.tsx'),
  join(ROOT, 'src/screens/tasks/InboxEntry.tsx'),
  join(ROOT, 'app/inbox.tsx'),
];

describe('inbox safety', () => {
  it('no approve-all, merge or always-allow anywhere in the inbox sources', () => {
    expect(SOURCES.length).toBeGreaterThan(8);
    for (const f of SOURCES) {
      expect(strip(readFileSync(f, 'utf8')), f).not.toMatch(/approveAll|approve[ _-]?all|merge|always[ _-]?allow|alwaysAllow/i);
    }
  });

  it('no HTML sinks, markdown, image, URL fetch, link opening or eval', () => {
    for (const f of SOURCES) {
      expect(strip(readFileSync(f, 'utf8')), f).not.toMatch(/dangerouslySetInnerHTML|innerHTML|WebView|<Image\b|\bfetch\s*\(|Linking\.openURL|\beval\s*\(|new\s+Function|Markdown/);
    }
  });

  it('the screen only snoozes/resolves keys of rows whose actions allow it', () => {
    // The sheet lists Snooze/Resolve only when row.actions has them; the model never gives a perm: row those.
    const sheet = strip(readFileSync(join(ROOT, 'src/screens/inbox/InboxActionSheet.tsx'), 'utf8'));
    expect(sheet).toMatch(/row\.actions\.includes\('snooze'\)/);
    expect(sheet).toMatch(/row\.actions\.includes\('resolve'\)/);
    const v = inboxView([inboxItem({ key: 'perm:r:q', kind: 'permission' }), inboxItem({ key: 'perm:r2:q2', kind: 'verdict' })], Date.now());
    const rows = v.kind === 'list' ? v.sections.flatMap((s) => s.rows) : [];
    expect(rows).toHaveLength(2);
    for (const r of rows) expect(r.actions.filter((a) => a === 'snooze' || a === 'resolve')).toEqual([]);
  });

  it('hostile strings pass through the row model verbatim', () => {
    const evil = '<img src=x onerror=alert(1)>';
    const v = inboxView([inboxItem({ title: evil, summary: evil })], Date.now());
    const row = v.kind === 'list' ? v.sections[0].rows[0] : null;
    expect(row?.title).toBe(evil);
    expect(row?.summary).toBe(evil);
  });
});
