// pluginSlashRpc.test.ts: PB.4, test for test with web's composer-slash-rpc.test.tsx. The phone executes nothing:
// it asks the daemon (plugin.call) and inserts the answer as plain text; it never sends.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { DaemonError } from '../relay/RelayConnection';
import { applySlashRpcPrompt, runSlashRpc, slashRpcPrompt } from '../plugins/slashRpc';
import { buildSlashCommands, pickSlashCommand, slashSuggestions } from '../plugins/slashCommands';
import { cmd, fakeConnection } from './pluginFixtures';

const rpcCmd = (plugin: string, name: string) => ({ plugin, name, qualified: `${plugin}:${name}`, description: `${name} desc`, rpc: 'gen' });

describe('RPC-backed slash commands (mobile)', () => {
  it('TestSlashRpc_InsertsNeverSends: the row carries the call, picking alone changes nothing, the answer lands as plain text', async () => {
    const [row] = buildSlashCommands([], [rpcCmd('acme', 'hello')]);
    expect(row.rpc).toEqual({ plugin: 'acme', rpc: 'gen', command: 'acme:hello' });
    expect(row.text).toBe('');
    expect(slashSuggestions([row], '/hel').map((r) => r.label)).toEqual(['hello']);
    expect(pickSlashCommand('/hel', row)).toBe('/hel'); // never blanks the draft by itself

    const text = 'Hello! <b>bold</b> **md** [x](javascript:alert(1))';
    const { conn, calls } = fakeConnection(() => ({ prompt: text }));
    const out = await runSlashRpc(conn, row);
    expect(calls).toEqual([['plugin.call', { pluginId: 'acme', rpc: 'gen', params: { command: 'acme:hello', args: '' } }]]);
    expect(out).toEqual({ ok: true, prompt: text });
    expect(applySlashRpcPrompt('/hel', '/hel', text)).toBe(text); // the exact characters
    expect(calls.map(([m]) => m)).toEqual(['plugin.call']); // nothing else was sent: no task.send, no run.start
  });

  it('a late answer for a draft that moved on is dropped', () => {
    expect(applySlashRpcPrompt('something else', '/hel', 'late')).toBeNull();
    expect(applySlashRpcPrompt('/hello', '/hel', 'late')).toBeNull();
    expect(applySlashRpcPrompt('/hel', '/hel', 'ok')).toBe('ok');
  });

  it('TestSlashRpc_ErrorShownInline: a typed daemon error and a malformed answer are outcomes for the list, never a throw or an insertion', async () => {
    const [row] = buildSlashCommands([], [rpcCmd('acme', 'hello')]);
    const failing = fakeConnection(() => {
      throw new DaemonError('plugin.call: plugin acme: rpc gen did not answer within 5s', 'call_timeout');
    });
    expect(await runSlashRpc(failing.conn, row)).toEqual({ ok: false, message: 'plugin acme: rpc gen did not answer within 5s', code: 'call_timeout' });
    for (const bad of [null, {}, { prompt: '' }, { prompt: 7 }, { prompt: 'a\u0000b' }, { prompt: 'x'.repeat(16 * 1024 + 1) }, 'text']) {
      expect(slashRpcPrompt(bad)).toBeNull();
      const out = await runSlashRpc(fakeConnection(() => bad).conn, row);
      expect(out.ok).toBe(false);
    }
    expect(await runSlashRpc(null, row)).toEqual({ ok: false, message: 'Not connected to the daemon.' });
    expect(slashRpcPrompt({ prompt: 'ok' })).toBe('ok');
  });

  it('TestSlashRpc_BuiltinAndCollisionRules: built-in wins the bare name; first plugin by id owns it between plugins', () => {
    const rows = buildSlashCommands([{ name: 'hello', description: 'b', text: 'BUILTIN' }], [rpcCmd('acme', 'hello')]);
    expect(rows.map((r) => r.label)).toEqual(['hello', 'acme:hello']);
    expect(rows[1].names).toEqual(['acme:hello']);
    const mixed = buildSlashCommands([], [cmd('zeta', 'go'), rpcCmd('alpha', 'go')]);
    expect(mixed.map((r) => [r.label, r.rpc ? 'rpc' : 'prompt'])).toEqual([
      ['go', 'rpc'],
      ['zeta:go', 'prompt'],
    ]);
  });

  it('the suggestion list draws the answer and errors as plain <Text>; nothing evaluates', () => {
    const src = readFileSync(join(__dirname, '..', 'screens', 'task', 'SlashSuggestions.tsx'), 'utf8');
    expect(src).not.toMatch(/dangerouslySetInnerHTML|WebView|eval\(|new Function/);
  });
});
