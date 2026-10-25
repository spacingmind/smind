// pluginSlash.test.ts: AM3, test for test with web's slash-commands.test.ts (the function is a port).

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { applySlashReplacement, buildSlashCommands, findActiveSlash, pickSlashCommand, rankSlashCommands, slashSuggestions } from '../plugins/slashCommands';
import { cmd } from './pluginFixtures';

describe('slash command list', () => {
  it('TestSlashCommands_BuiltinWinsCollision: a built-in keeps the bare name; the plugin command is /<plugin>:<name>', () => {
    const rows = buildSlashCommands([{ name: 'review', description: 'built-in review', text: 'BUILTIN' }], [cmd('acme', 'review'), cmd('acme', 'plan')]);
    expect(rows.map((r) => r.label)).toEqual(['review', 'acme:review', 'plan']);
    expect(rows[0].builtin).toBe(true);
    expect(rows[1].names).toEqual(['acme:review']);
    expect(rankSlashCommands(rows, 'acme:rev').map((r) => r.key)).toEqual(['plugin:acme:review']);
    expect(rankSlashCommands(rows, 'review').map((r) => r.key)).toEqual(['builtin:review', 'plugin:acme:review']);
  });

  it('TestSlashCommands_FirstPluginByIdWinsBetweenPlugins (input order is irrelevant)', () => {
    const rows = buildSlashCommands([], [cmd('zeta', 'review'), cmd('alpha', 'review'), cmd('zeta', 'only')]);
    expect(rows.map((r) => r.key)).toEqual(['plugin:alpha:review', 'plugin:zeta:review', 'plugin:zeta:only']);
    expect(rows.map((r) => r.label)).toEqual(['review', 'zeta:review', 'only']);
  });

  it('TestSlashCommands_NoPluginCommands_ComposerUnchanged: nothing to list, nothing to open', () => {
    expect(buildSlashCommands([], [])).toEqual([]);
    expect(slashSuggestions([], '/')).toEqual([]);
    expect(slashSuggestions([], '/anything')).toEqual([]);
    expect(slashSuggestions([], 'plain text')).toEqual([]);
  });

  it('ranks prefix matches before inner matches; an empty query lists all; no match is empty', () => {
    const rows = buildSlashCommands([], [cmd('p', 'review'), cmd('p', 'preview'), cmd('p', 'plan')]);
    expect(rankSlashCommands(rows, '').length).toBe(3);
    expect(rankSlashCommands(rows, 'rev').map((r) => r.label)).toEqual(['review', 'preview']);
    expect(rankSlashCommands(rows, 'zzz')).toEqual([]);
  });
});

describe('findActiveSlash', () => {
  it('only a leading /token with the caret inside it opens the list', () => {
    expect(findActiveSlash({ text: '/', cursorIndex: 1 })).toEqual({ start: 0, end: 1, query: '' });
    expect(findActiveSlash({ text: '/rev', cursorIndex: 4 })).toEqual({ start: 0, end: 4, query: 'rev' });
    expect(findActiveSlash({ text: '/rev', cursorIndex: 2 })).toEqual({ start: 0, end: 2, query: 'r' });
    expect(findActiveSlash({ text: '/rev now', cursorIndex: 8 })).toBeNull();
    expect(findActiveSlash({ text: 'see /usr/bin', cursorIndex: 12 })).toBeNull();
    expect(findActiveSlash({ text: 'http://x', cursorIndex: 8 })).toBeNull();
    expect(findActiveSlash({ text: '/rev', cursorIndex: 0 })).toBeNull();
    expect(findActiveSlash({ text: '', cursorIndex: 0 })).toBeNull();
  });
});

describe('applySlashReplacement', () => {
  it('replaces the /token with the prompt and puts the caret after it', () => {
    const [row] = buildSlashCommands([], [cmd('a', 'go', 'Do the thing.')]);
    const slash = findActiveSlash({ text: '/g tail', cursorIndex: 2 })!;
    expect(applySlashReplacement({ text: '/g tail', slash, command: row })).toEqual({ text: 'Do the thing. tail', cursor: 13 });
  });
});

describe('TestSlashCommands_Autocomplete_InsertsNeverSends', () => {
  const rows = buildSlashCommands([], [cmd('acme', 'review', 'Review the diff.'), cmd('acme', 'plan', 'Plan it.')]);

  it('a / draft lists the commands; picking returns the prompt text as the new draft', () => {
    expect(slashSuggestions(rows, '/').map((r) => r.label)).toEqual(['review', 'plan']);
    const picked = pickSlashCommand('/rev', slashSuggestions(rows, '/rev')[0]);
    expect(picked).toBe('Review the diff.');
    expect(slashSuggestions(rows, picked)).toEqual([]);
  });

  it('the suggestion list component has no send path in scope (it takes rows and onPick only)', () => {
    const src = readFileSync(join(__dirname, '..', 'screens', 'task', 'SlashSuggestions.tsx'), 'utf8');
    expect(src).not.toMatch(/send/i);
    const composer = readFileSync(join(__dirname, '..', 'screens', 'task', 'Composer.tsx'), 'utf8');
    const pick = composer.slice(composer.indexOf('const pickSlash'), composer.indexOf('return (', composer.indexOf('const pickSlash')));
    expect(pick).toContain('onChangeDraft');
    expect(pick).not.toMatch(/onSend|onSendNow|run\.start/);
  });
});
