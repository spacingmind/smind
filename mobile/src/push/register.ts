// The opt-in push registration flow (ADR-0040 section 3), fully injected so it
// runs under node. Nothing here may log: the token and key are secrets.

import { base64Encode } from '../relay/base64';
import type { PushKeyStore } from './keys';

export type PushKind = 'permission' | 'needs_continue' | 'done' | 'error' | 'stalled';
export const ALL_KINDS: readonly PushKind[] = ['permission', 'needs_continue', 'done', 'error', 'stalled'];

export interface RegisterDeps {
  platform: 'android' | 'ios';
  /** This install's relay device id. */
  deviceId: string;
  /** Creates the Android channel "attention"; a no-op elsewhere. */
  ensureChannel(): Promise<void>;
  /** OS notification permission. */
  requestPermission(): Promise<boolean>;
  /** The Expo push token, null when unavailable. */
  getToken(): Promise<string | null>;
  /** RelayConnection.call. */
  call(method: string, params?: unknown): Promise<unknown>;
  keys: PushKeyStore;
  randomBytes(n: number): Uint8Array;
}

export type EnableResult =
  | { ok: true; kinds: string[] }
  | { ok: false; reason: 'permission_denied' | 'no_token' | 'rpc_failed'; message?: string };

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

async function fetchToken(deps: RegisterDeps): Promise<string | null> {
  try {
    const token = await deps.getToken();
    return token ? token : null;
  } catch {
    return null;
  }
}

async function register(
  deps: RegisterDeps,
  token: string,
  key: Uint8Array,
  kinds: readonly PushKind[],
): Promise<EnableResult> {
  try {
    const result = await deps.call('push.register', {
      v: 1,
      provider: 'expo',
      token,
      platform: deps.platform,
      key: base64Encode(key),
      kinds: [...kinds],
    });
    const echoed = (result as { kinds?: unknown } | null | undefined)?.kinds;
    if (Array.isArray(echoed) && echoed.every((k) => typeof k === 'string')) {
      return { ok: true, kinds: echoed as string[] };
    }
    return { ok: true, kinds: [...kinds] };
  } catch (e) {
    return { ok: false, reason: 'rpc_failed', message: errorMessage(e) };
  }
}

export async function enablePush(deps: RegisterDeps, kinds: readonly PushKind[] = ALL_KINDS): Promise<EnableResult> {
  // The Android permission prompt only appears once a channel exists.
  await deps.ensureChannel();
  if (!(await deps.requestPermission())) return { ok: false, reason: 'permission_denied' };
  const token = await fetchToken(deps);
  if (token === null) return { ok: false, reason: 'no_token' };

  // Store the key before the RPC so a push arriving right after the daemon
  // saves it can already be opened.
  const key = deps.randomBytes(32);
  await deps.keys.put(deps.deviceId, key);
  const result = await register(deps, token, key, kinds);
  if (!result.ok) await deps.keys.delete(deps.deviceId);
  return result;
}

export async function reregisterToken(
  deps: RegisterDeps,
  kinds: readonly PushKind[] = ALL_KINDS,
): Promise<EnableResult | null> {
  const key = await deps.keys.get(deps.deviceId);
  if (key === null) return null;
  const token = await fetchToken(deps);
  if (token === null) return { ok: false, reason: 'no_token' };
  return register(deps, token, key, kinds);
}

export async function disablePush(deps: RegisterDeps): Promise<void> {
  try {
    await deps.call('push.unregister', {});
  } catch {
    // The daemon prunes dead registrations itself; the local key must go regardless.
  }
  await deps.keys.delete(deps.deviceId);
}
