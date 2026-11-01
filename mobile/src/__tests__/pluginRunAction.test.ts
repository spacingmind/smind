// pluginRunAction.test.ts: the pure view-action executor with a recording
// fake deps. The guarantees under test: a document-supplied identity is
// never trusted (the caller's pluginId, the panel list, the url's host
// recomputed from the url), text only ever arrives as a composer insert,
// and a url only opens after a confirm. The runner never throws: a
// failure is a typed outcome.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';

import { DaemonError } from '../relay/RelayConnection';
import { runViewAction, ActionDeps } from '../plugins/view/runAction';

/** A deps whose every effect is recorded as a string (plus the rpc requests); overrides replace one effect at a time. */
function recordingDeps(overrides: Partial<ActionDeps> = {}) {
  const calls: string[] = [];
  const rpcReqs: { pluginId: string; rpc: string; params: Record<string, unknown> }[] = [];
  const deps: ActionDeps = {
    pluginId: 'acme',
    panelIds: ['overview', 'logs'],
    commands: [
      { plugin: 'acme', name: 'summarise', qualified: 'acme:summarise', description: 'd', prompt: 'Summarise this task' },
      { plugin: 'acme', name: 'gen', qualified: 'acme:gen', description: 'd', rpc: 'gen' }, // rpc command: no prompt to insert
      { plugin: 'other', name: 'summarise', qualified: 'other:summarise', description: 'd', prompt: 'From another plugin' },
    ],
    callRpc: async (req) => {
      rpcReqs.push(req);
      calls.push(`callRpc ${req.pluginId} ${req.rpc} ${JSON.stringify(req.params)}`);
      return { ok: true };
    },
    insertPrompt: (text) => {
      calls.push(`insertPrompt ${JSON.stringify(text)}`);
      return true;
    },
    openPanel: (panelId, context) => {
      calls.push(`openPanel ${panelId}${context === undefined ? '' : ` ${JSON.stringify(context)}`}`);
    },
    confirmOpenUrl: async (host, url) => {
      calls.push(`confirmOpenUrl ${host} ${url}`);
      return true;
    },
    openUrl: async (url) => {
      calls.push(`openUrl ${url}`);
    },
    refresh: () => {
      calls.push('refresh');
    },
    ...overrides,
  };
  return { deps, calls, rpcReqs };
}

describe('plugin view action runner', () => {
  it('rpc calls plugin.call params on the same plugin and refreshes', async () => {
    const { deps, calls, rpcReqs } = recordingDeps();
    const out = await runViewAction({ kind: 'rpc', rpc: 'refresh', params: { depth: 2 }, pluginId: 'evil' }, deps, { formValues: { title: 'Bug', urgent: true } });
    expect(out).toEqual({ kind: 'done' });
    expect(rpcReqs).toEqual([{ pluginId: 'acme', rpc: 'refresh', params: { depth: 2, values: { title: 'Bug', urgent: true } } }]); // the caller's pluginId, the document's ignored
    expect(calls).toEqual(['callRpc acme refresh {"depth":2,"values":{"title":"Bug","urgent":true}}', 'refresh']); // the panel refetches after the rpc
  });

  it('rpc error is a typed outcome with code', async () => {
    const failing = recordingDeps({ callRpc: async () => { throw new DaemonError('plugin.call: rpc refresh did not answer', 'call_timeout'); } });
    expect(await runViewAction({ kind: 'rpc', rpc: 'refresh' }, failing.deps)).toEqual({ kind: 'error', message: 'rpc refresh did not answer', code: 'call_timeout' });
    expect(failing.calls).toEqual([]); // no refresh after a failure
    const plain = recordingDeps({ callRpc: async () => { throw new Error('boom'); } });
    expect(await runViewAction({ kind: 'rpc', rpc: 'refresh' }, plain.deps)).toEqual({ kind: 'error', message: 'boom', code: undefined });
  });

  it('prompt-action-inserts-not-sends', async () => {
    const { deps, calls } = recordingDeps();
    expect(await runViewAction({ kind: 'prompt', text: 'Why did the suite slow down?' }, deps)).toEqual({ kind: 'done' });
    expect(calls).toEqual(['insertPrompt "Why did the suite slow down?"']); // only an insert: callRpc (plugin.call) is never touched
    const refusing = recordingDeps({ insertPrompt: () => false });
    expect(await runViewAction({ kind: 'prompt', text: 'x' }, refusing.deps)).toEqual({ kind: 'refused', reason: 'Prompt text was empty or too large' });
  });

  it('command resolves only a qualified command of the same plugin', async () => {
    const mine = recordingDeps();
    expect(await runViewAction({ kind: 'command', name: 'acme:summarise' }, mine.deps)).toEqual({ kind: 'done' });
    expect(mine.calls).toEqual(['insertPrompt "Summarise this task"']);
    const foreign = recordingDeps(); // the same qualified name belongs to another plugin: not resolvable from acme's panel
    expect(await runViewAction({ kind: 'command', name: 'other:summarise' }, foreign.deps)).toEqual({ kind: 'refused', reason: 'Unknown command' });
    const rpcCommand = recordingDeps(); // acme's rpc command has no prompt of its own to insert
    expect(await runViewAction({ kind: 'command', name: 'acme:gen' }, rpcCommand.deps)).toEqual({ kind: 'refused', reason: 'Unknown command' });
  });

  it('unknown command and unknown panel are refused', async () => {
    const { deps, calls } = recordingDeps();
    expect(await runViewAction({ kind: 'command', name: 'acme:nope' }, deps)).toEqual({ kind: 'refused', reason: 'Unknown command' });
    expect(await runViewAction({ kind: 'openPanel', panel: 'secret' }, deps)).toEqual({ kind: 'refused', reason: 'Unknown panel' });
    expect(calls).toEqual([]); // nothing ran
  });

  it('openPanel passes context for a declared panel', async () => {
    const { deps, calls } = recordingDeps();
    expect(await runViewAction({ kind: 'openPanel', panel: 'logs', context: { runId: 'r7' } }, deps)).toEqual({ kind: 'done' });
    expect(calls).toEqual(['openPanel logs {"runId":"r7"}']);
  });

  it('openUrl-confirm-shows-host', async () => {
    const raw = { kind: 'openUrl', url: 'https://good.example:8443/p', host: 'evil.example' }; // the document's host is a lie
    const accepted = recordingDeps();
    expect(await runViewAction(raw, accepted.deps)).toEqual({ kind: 'done' });
    expect(accepted.calls).toEqual(['confirmOpenUrl good.example:8443 https://good.example:8443/p', 'openUrl https://good.example:8443/p']);
    const seen: { host: string; url: string }[] = [];
    const declined = recordingDeps({ confirmOpenUrl: async (host, url) => { seen.push({ host, url }); return false; } });
    expect(await runViewAction(raw, declined.deps)).toEqual({ kind: 'cancelled' });
    expect(seen).toEqual([{ host: 'good.example:8443', url: 'https://good.example:8443/p' }]); // declined confirm also saw the recomputed host
    expect(declined.calls).toEqual([]); // declined: openUrl never called
  });

  it('openUrl javascript: and userinfo urls are refused without a confirm', async () => {
    for (const url of ['javascript:alert(1)', 'https://user:pw@example.com/', 'file:///etc/passwd']) {
      const { deps, calls } = recordingDeps();
      const out = await runViewAction({ kind: 'openUrl', url, host: 'example.com' }, deps);
      expect(out.kind, url).toBe('refused');
      expect(calls, url).toEqual([]); // no confirm dialog, no open
    }
  });

  it('unchecked shapes are refused', async () => {
    const { deps } = recordingDeps();
    for (const raw of [null, { kind: 'eval', code: 'process.exit(1)' }, { kind: 'rpc' }]) {
      expect(await runViewAction(raw, deps), JSON.stringify(raw)).toEqual({ kind: 'refused', reason: 'Unrecognised action' });
    }
  });

  it('TaskDetailScreen only inserts the inbox text as a draft', () => {
    const source = readFileSync(join(__dirname, '..', 'screens', 'TaskDetailScreen.tsx'), 'utf8');
    expect(source).toContain("import { takePendingInsert } from '../plugins/composerInbox';");
    const at = source.indexOf('takePendingInsert()');
    expect(at).toBeGreaterThanOrEqual(0);
    const effect = source.slice(at).split('\n').slice(0, 9).join('\n'); // the call's line and the next 8
    expect(effect.includes('onSend')).toBe(false); // the inbox lands as a draft only: nothing near it sends
    expect(effect.includes('sendPrompt')).toBe(false);
  });
});
