// grantsView.test.ts: the words and rules of the saved-grant UI, and that the copy matches the web's.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import type { GrantOffer, SavedGrant } from '../grants/types';
import {
  GRANT_BUTTON_LABEL,
  GRANT_NEEDS_OPT_IN_HINT,
  GRANT_OPT_IN_FALLBACK,
  grantAllowProjectCode,
  grantClassLabel,
  grantExpiryText,
  grantNeedsOptIn,
  grantRevokeCopy,
  grantRows,
  grantSaveEnabled,
  grantStatusHint,
  grantStatusLabel,
} from '../grants/view';

const NOW = Date.parse('2026-11-15T12:00:00Z');
const READONLY: GrantOffer = { command: 'git status', project: 'smind', folder: '/repo', expiresInDays: 30, class: 'readonly', requiresOptIn: false, note: 'n' };
const RUNNER: GrantOffer = { ...READONLY, command: 'make build', class: 'runner', requiresOptIn: true };

describe('the opt-in rule (the daemon is the guard; this is the human view of it)', () => {
  it('a read-only offer saves with no opt-in and sends allowProjectCode false', () => {
    expect(grantNeedsOptIn(READONLY)).toBe(false);
    expect(grantSaveEnabled(READONLY, false, false)).toBe(true);
    expect(grantAllowProjectCode(READONLY, false)).toBe(false);
    expect(grantAllowProjectCode(READONLY, true)).toBe(false); // an opt-in means nothing on a read-only grant
  });

  it('a runner is disabled until the opt-in is ticked, and then sends allowProjectCode true', () => {
    expect(grantNeedsOptIn(RUNNER)).toBe(true);
    expect(grantSaveEnabled(RUNNER, false, false)).toBe(false);
    expect(grantSaveEnabled(RUNNER, true, false)).toBe(true);
    expect(grantAllowProjectCode(RUNNER, true)).toBe(true);
    expect(grantAllowProjectCode(RUNNER, false)).toBe(false);
  });

  it('the class decides, both ways: an unknown class or a runner flagged as needing no opt-in still needs it; a read-only offer flagged as needing one does not', () => {
    const runnerNoFlag: GrantOffer = { ...RUNNER, requiresOptIn: false };
    const readonlyFlagged: GrantOffer = { ...READONLY, requiresOptIn: true };
    expect(grantNeedsOptIn({ class: 'mystery' })).toBe(true);
    expect(grantNeedsOptIn(runnerNoFlag)).toBe(true);
    expect(grantSaveEnabled({ class: 'mystery' }, false, false)).toBe(false);
    expect(grantNeedsOptIn(readonlyFlagged)).toBe(false);
    expect(grantAllowProjectCode(readonlyFlagged, true)).toBe(false);
  });

  it('nothing saves while an answer is in flight', () => {
    expect(grantSaveEnabled(READONLY, false, true)).toBe(false);
    expect(grantSaveEnabled(RUNNER, true, true)).toBe(false);
  });
});

describe('words', () => {
  it('names the class as text and every status with what it means', () => {
    expect(grantClassLabel('readonly')).toBe('Read-only');
    expect(grantClassLabel('runner')).toBe('Runs project code');
    expect(grantClassLabel('whatever')).toBe('Runs project code');
    expect(grantStatusLabel('void')).toBe('Void: tool setup changed');
    expect(grantStatusLabel('odd')).toBe('odd');
    expect(grantStatusHint({ status: 'active' })).toBe('');
    for (const s of ['expired', 'revoked', 'void']) expect(grantStatusHint({ status: s })).toMatch(/asks again/);
  });

  it('writes expiry from the clock', () => {
    expect(grantExpiryText('2026-12-10T12:00:00Z', NOW)).toBe('Expires in 25d');
    expect(grantExpiryText('2026-11-15T15:30:00Z', NOW)).toBe('Expires in 3h');
    expect(grantExpiryText('2026-11-13T12:00:00Z', NOW)).toBe('Expired 2d ago');
    expect(grantExpiryText('not a date', NOW)).toBe('not a date');
  });

  it('builds a row with every line a plain string and Revoke only until revoked', () => {
    const g = (over: Partial<SavedGrant>): SavedGrant => ({
      id: 1, workspaceId: 2, command: 'make build', folder: '/repo', class: 'runner', runnerOptIn: true, createdAt: '2026-11-13T12:00:00Z', expiresAt: '2026-12-13T12:00:00Z', createdBy: 'local', useCount: 1, lastUsedAt: '2026-11-15T11:00:00Z', status: 'active', ...over,
    });
    const [a, b, c] = grantRows([g({}), g({ id: 2, class: 'readonly', runnerOptIn: false, useCount: 0, lastUsedAt: undefined }), g({ id: 3, status: 'revoked', runnerOptIn: false })], NOW);
    expect(a).toMatchObject({ isRunner: true, canRevoke: true, folderLine: 'Project folder: /repo', usageLine: 'Used 1 time, last 1h ago. Saved 2d ago.' });
    expect(a.optInLine).toMatch(/You allowed this to run the project's code/);
    expect(b).toMatchObject({ isRunner: false, optInLine: null, usageLine: 'Used 0 times. Saved 2d ago.' });
    expect(c).toMatchObject({ canRevoke: false });
    expect(c.optInLine).toBe('No project-code opt-in: this is never applied.');
    expect(grantRevokeCopy('make build')).toBe('make build will ask again in this project.');
  });
});

// The copy is the web's: read its source and compare, so the two cannot drift.
describe('copy is identical to the web', () => {
  const web = readFileSync(join(__dirname, '..', '..', '..', 'web', 'packages', 'ui', 'src', 'lib', 'grant-copy.ts'), 'utf8');
  const constant = (name: string) => new RegExp(`export const ${name} = "((?:[^"\\\\]|\\\\.)*)";`).exec(web)?.[1];
  it('the control and opt-in words', () => {
    expect(constant('GRANT_BUTTON_LABEL')).toBe(GRANT_BUTTON_LABEL);
    expect(constant('GRANT_NEEDS_OPT_IN_HINT')).toBe(GRANT_NEEDS_OPT_IN_HINT);
    expect(constant('GRANT_OPT_IN_FALLBACK')).toBe(GRANT_OPT_IN_FALLBACK);
  });
  it('the class and status words', () => {
    expect(web).toContain('"Read-only"');
    expect(web).toContain('"Runs project code"');
    expect(web).toContain('"Void: tool setup changed"');
    expect(web).toContain('"The project\'s MCP servers changed, so this grant is not applied; this command asks again."');
    expect(web).toContain('"Not applied; this command asks again."');
  });
});
