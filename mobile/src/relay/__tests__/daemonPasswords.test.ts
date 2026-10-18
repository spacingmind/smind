// daemonPasswords.test.ts: the in-memory daemon password store (ADR-0031 P13c)
// and the guard that the files which handle a password never reach for any
// persistence, logging or analytics API.

import { readFileSync } from 'fs';
import { join } from 'path';
import { describe, expect, it } from 'vitest';
import { base64UrlEncode } from '../base64';
import { createDaemonPasswords, MAX_PASSWORD_BYTES, passwordKey, passwordProblem } from '../daemonPasswords';
import { describePasswordState } from '../connectionStatus';
import { parsePairingURL } from '../pairing';

function offer(pubByte: number, host = 'host-1', relay = 'https://relay.test') {
  const wire = { v: 1, id: 'd', pk: base64UrlEncode(new Uint8Array(32).fill(pubByte)), relay, sec: base64UrlEncode(new Uint8Array(32).fill(9)), ws: host };
  return parsePairingURL('https://x/pair#offer=' + base64UrlEncode(new TextEncoder().encode(JSON.stringify(wire))));
}

describe('daemon password store', () => {
  it('holds a password per key and forgets only exactly the value it is told to', () => {
    const store = createDaemonPasswords();
    store.set('k', 'one');
    store.forgetIfEqual('k', 'other');
    expect(store.get('k')).toBe('one');
    store.set('k', 'two'); // typed while a reply to 'one' was in flight
    store.forgetIfEqual('k', 'one'); // the daemon refused 'one'
    expect(store.get('k')).toBe('two');
    store.forgetIfEqual('k', 'two');
    expect(store.get('k')).toBeUndefined();
  });

  it('keys by relay address, host id and daemon public key', () => {
    const base = passwordKey(offer(1));
    expect(passwordKey(offer(1))).toBe(base);
    expect(passwordKey(offer(2))).not.toBe(base); // another daemon key
    expect(passwordKey(offer(1, 'host-2'))).not.toBe(base); // another relay host
    expect(passwordKey(offer(1, 'host-1', 'https://other.test'))).not.toBe(base); // another relay
  });

  it('does not expose its contents when serialised or inspected', () => {
    const store = createDaemonPasswords();
    store.set('k', 'TOP-SECRET');
    expect(JSON.stringify(store)).not.toContain('TOP-SECRET');
    expect(Object.keys(store).sort()).toEqual(['forgetIfEqual', 'get', 'set']);
    expect(String(store)).not.toContain('TOP-SECRET');
  });

  it('measures the 128-byte limit in UTF-8 bytes and refuses empty', () => {
    expect(passwordProblem('')).toContain('Enter');
    expect(passwordProblem('a'.repeat(MAX_PASSWORD_BYTES))).toBeNull();
    expect(passwordProblem('a'.repeat(MAX_PASSWORD_BYTES + 1))).toContain('128');
    expect(passwordProblem('é'.repeat(65))).toContain('128'); // 130 bytes, 65 characters
    expect(passwordProblem('  spaces are part of a password  ')).toBeNull();
  });
});

describe('password prompt copy', () => {
  it('says required, incorrect and locked (with a countdown), and nothing for other states', () => {
    expect(describePasswordState({ kind: 'password', reason: 'required' })).toMatchObject({ title: 'Daemon password required', tone: 'warning', locked: false });
    expect(describePasswordState({ kind: 'password', reason: 'incorrect' })).toMatchObject({ title: 'Incorrect password', tone: 'danger', locked: false });
    const locked = describePasswordState({ kind: 'locked', retryAfter: 8, until: 10_000 }, 4_500)!;
    expect(locked).toMatchObject({ title: 'Too many attempts', locked: true });
    expect(locked.detail).toContain('6 s'); // ceil((10000 - 4500) / 1000)
    expect(describePasswordState({ kind: 'locked', retryAfter: 8, until: 10_000 }, 20_000)!.detail).toContain('0 s');
    expect(describePasswordState({ kind: 'connected' })).toBeNull();
    expect(describePasswordState({ kind: 'pending', deviceId: 'x', label: 'y' })).toBeNull();
  });
});

describe('a password is memory only', () => {
  // Code only: the comments explain what is NOT done and name the APIs by design.
  const code = (rel: string) =>
    readFileSync(join(__dirname, '..', '..', '..', rel), 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, '')
      .replace(/^\s*\/\/.*$/gm, '');
  const read = code;

  it('the files that handle it import no persistence and call no logging or analytics API', () => {
    for (const rel of ['src/relay/daemonPasswords.ts', 'src/screens/DaemonPasswordPrompt.tsx', 'src/relay/RelayConnection.ts', 'src/screens/PairingScreen.tsx', 'App.tsx']) {
      const src = read(rel);
      expect(src, rel).not.toMatch(/expo-secure-store|async-storage|AsyncStorage|localStorage|sessionStorage|expo-file-system|expo-sqlite|MMKV/);
      expect(src, rel).not.toMatch(/console\.(log|info|warn|error|debug)/);
      expect(src, rel).not.toMatch(/analytics|crashlytics|sentry|segment|mixpanel/i);
    }
  });

  it('the secure-store modules never mention the password', () => {
    for (const rel of ['src/relay/deviceKeyStore.ts', 'src/relay/platformKeyStore.ts']) expect(read(rel).toLowerCase(), rel).not.toContain('password');
  });
});
