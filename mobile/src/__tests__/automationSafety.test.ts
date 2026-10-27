// automationSafety.test.ts: no eval / new Function in the automation code, daemon strings are not interpreted, and the
// automation screens render them only through <Text>.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { auditRowView, decisionChip } from '../automations/historyView';
import { automationsListView, signalSummary } from '../automations/listView';
import { automation, auditRow, listing } from './automationFixtures';

const ROOT = join(__dirname, '..', '..');

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

const BANNED = /\beval\s*\(|\bnew\s+Function\b|\bFunction\s*\(\s*['"`]/;
const SCREENS = ['src/screens/AutomationsScreen.tsx', 'src/screens/AutomationDetailScreen.tsx', 'src/screens/automations/AutomationRow.tsx', 'src/screens/automations/HistoryRow.tsx', 'app/automations.tsx', 'app/automation/[id].tsx'];

describe('no eval / new Function', () => {
  it('in the automation sources and screens', () => {
    const all = [...files(join(ROOT, 'src', 'automations')), ...SCREENS.map((s) => join(ROOT, s)), join(ROOT, 'src/ui/confirm.ts')];
    expect(all.length).toBeGreaterThan(8);
    expect(all.filter((f) => BANNED.test(strip(readFileSync(f, 'utf8'))))).toEqual([]);
  });
});

describe('screens render daemon text as plain text', () => {
  it('no HTML sinks, markdown, image, URL fetch, link opening or dynamic import', () => {
    for (const rel of SCREENS) {
      expect(strip(readFileSync(join(ROOT, rel), 'utf8')), rel).not.toMatch(/dangerouslySetInnerHTML|innerHTML|WebView|<Image\b|Image\.|source=\{\{\s*uri|\bfetch\s*\(|XMLHttpRequest|Linking\.openURL|import\(|require\(|Markdown/);
    }
  });

  it('hostile strings pass through the view models verbatim', () => {
    const evil = ['<img src=x onerror=alert(1)>', '[click](javascript:alert(1))', '![x](https://evil.example/p.png)', '<script>alert(1)</script>', '{{constructor.constructor("x")()}}', '‮gnp.exe'];
    for (const s of evil) {
      const v = automationsListView(listing([automation({ name: s, status: 'paused', pausedReason: s, signal: { kind: 'ci', workflow: s, branch: s } })]));
      const row = v.kind === 'list' ? v.rows[0] : null;
      expect(row?.name).toBe(s);
      expect(row?.pausedReason).toBe(s);
      expect(row?.signal).toBe(`${s} on ${s}`);
      expect(signalSummary({ kind: 'cron', cron: s })).toBe(`${s} UTC`);
      const h = auditRowView(auditRow({ decision: s, reason: s, outcome: s, note: s }));
      expect(h.reason).toBe(s);
      expect(h.outcome).toBe(s);
      expect(h.decision).toBe(s);
      expect(decisionChip(s).label).toBe(s);
    }
  });
});
