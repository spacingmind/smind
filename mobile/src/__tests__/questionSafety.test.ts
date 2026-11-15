// questionSafety.test.ts: the mobile question card renders untrusted agent text as inert text and never offers a
// standing decision (ADR-0055 sections 7 and 8): no link opening, no markdown, no approve/merge/always-allow wording.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = join(__dirname, '..', '..');
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
const SOURCES = [
  'src/questionAnswer.ts',
  'src/questionRequests.ts',
  'src/questionView.ts',
  'src/questionCopy.ts',
  'src/screens/task/QuestionCard.tsx',
  'src/screens/task/QuestionMarker.tsx',
].map((f) => ({ f, text: strip(readFileSync(join(ROOT, f), 'utf8')) }));

describe('question card safety', () => {
  it('no source names merge, approve or always-allow', () => {
    for (const { f, text } of SOURCES) expect(text, f).not.toMatch(/merge|approve|always[ _-]?allow/i);
  });

  it('no source opens a link or renders markdown', () => {
    for (const { f, text } of SOURCES) {
      expect(text, f).not.toMatch(/Linking|openURL|dataDetector|autolink/i);
      expect(text, f).not.toMatch(/from '\.\.\/\.\.\/(screens\/task\/)?Markdown'|from '\.\/Markdown'|markdown/i);
    }
  });

  it('the card never preselects: no initial draft carries an option id', () => {
    const card = SOURCES.find((s) => s.f.endsWith('QuestionCard.tsx'))!.text;
    expect(card).toMatch(/useState<Record<string, Draft>>\(\{\}\)/);
  });
});
