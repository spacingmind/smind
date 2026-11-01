// stewardSafety.test.ts: the steward client is read-only -- it sends only steward.get and
// steward.frame, and a malformed answer degrades to empty lists.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { fetchFrame, fetchReview, STEWARD_READ_METHODS } from '../steward/api';
import { fakeConnection } from './pluginFixtures';

describe('steward client is read-only', () => {
  it('steward client sends only read methods', async () => {
    const { conn, calls } = fakeConnection((method) => {
      if (method === 'steward.get') return { id: 5, taskId: 7, headSha: 'abcdef1234' };
      return { contentType: 'image/png', data: 'aGk=' };
    });
    const review = await fetchReview(conn, 5);
    const frame = await fetchFrame(conn, 5, 'a.png');
    expect(calls).toEqual([
      ['steward.get', { reviewId: 5 }],
      ['steward.frame', { reviewId: 5, name: 'a.png' }],
    ]);
    expect(calls.every(([m]) => (STEWARD_READ_METHODS as readonly string[]).includes(m))).toBe(true);
    expect(review.findings).toEqual([]);
    expect(review.gates).toEqual([]);
    expect(frame).toEqual({ contentType: 'image/png', data: 'aGk=' });
  });
});

const ROOT = join(__dirname, '..', '..');
function files(dir: string): string[] {
  return readdirSync(dir).flatMap((e) => {
    const full = join(dir, e);
    return statSync(full).isDirectory() ? files(full) : /\.(ts|tsx)$/.test(e) ? [full] : [];
  });
}
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
const SOURCES = [
  ...files(join(ROOT, 'src', 'steward')),
  ...files(join(ROOT, 'src', 'policy')),
  ...files(join(ROOT, 'src', 'screens', 'steward')),
  join(ROOT, 'src/screens/StewardReviewScreen.tsx'),
  join(ROOT, 'src/screens/PolicyScreen.tsx'),
  join(ROOT, 'src/screens/task/PermissionPolicyLine.tsx'),
  join(ROOT, 'src/transcript/permissionPolicyLine.ts'),
  join(ROOT, 'src/ui/VerdictChip.tsx'),
  join(ROOT, 'app/review/[id].tsx'),
  join(ROOT, 'app/policy.tsx'),
];

describe('steward mobile safety', () => {
  it('steward mobile sources have no merge or approve control', () => {
    expect(SOURCES.length).toBeGreaterThanOrEqual(14);
    for (const f of SOURCES) {
      expect(strip(readFileSync(f, 'utf8')), f).not.toMatch(/merge|approve|always[ _-]?allow|alwaysAllow/i);
    }
  });

  it('steward mobile sources draw daemon text only as Text and never open links or run code', () => {
    for (const f of SOURCES) {
      expect(
        strip(readFileSync(f, 'utf8')),
        f,
      ).not.toMatch(/dangerouslySetInnerHTML|innerHTML|WebView|\bfetch\s*\(|Linking\.openURL|\beval\s*\(|new\s+Function|Markdown/);
    }
  });

  it('the scanner catches a planted always-allow control', () => {
    expect(strip('<Button label="Always allow" />')).toMatch(/merge|approve|always[ _-]?allow|alwaysAllow/i);
    expect(strip('// merge\nconst x = 1')).not.toMatch(/merge|approve|always[ _-]?allow|alwaysAllow/i);
  });
});
