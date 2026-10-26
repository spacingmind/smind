// Decrypts an ADR-0040 push blob (the Go side is internal/push/seal.go). Pure
// TypeScript: no react-native or expo imports, so it runs under node/vitest.

import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { base64UrlDecode } from '../relay/base64';

export interface PushEvent {
  id: string;
  kind: 'permission' | 'needs_continue' | 'done' | 'error' | 'test';
  taskId: number;
  runId?: string;
  chatId?: number;
  /** An automation alert (an auto-pause) that has no task to open: the tap opens this automation. */
  automationId?: number;
  title: string;
  body: string;
  at: number;
}

export type OpenFailure = 'key' | 'version' | 'malformed' | 'auth';
export type OpenResult = { ok: true; event: PushEvent } | { ok: false; error: OpenFailure };
export interface PushKeyEntry {
  deviceId: string;
  key: Uint8Array;
}

export const GENERIC_NOTIFICATION = { title: 'smind', body: 'Open smind to see what happened' } as const;
export const MAX_AGE_SECONDS = 86400;

const AAD_PREFIX = 'smind-push-v1';
const KINDS = ['permission', 'needs_continue', 'done', 'error', 'test'];

function fail(error: OpenFailure): OpenResult {
  return { ok: false, error };
}

function isSafeInt(v: unknown): v is number {
  return typeof v === 'number' && Number.isSafeInteger(v);
}

function toEvent(v: unknown): PushEvent | null {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.id !== 'string' || typeof o.title !== 'string' || typeof o.body !== 'string') return null;
  if (typeof o.kind !== 'string' || !KINDS.includes(o.kind)) return null;
  if (!isSafeInt(o.taskId) || o.taskId < 0) return null;
  if (typeof o.at !== 'number' || !Number.isFinite(o.at)) return null;
  if (o.runId !== undefined && typeof o.runId !== 'string') return null;
  if (o.chatId !== undefined && !isSafeInt(o.chatId)) return null;
  if (o.automationId !== undefined && (!isSafeInt(o.automationId) || o.automationId < 0)) return null;
  const event: PushEvent = {
    id: o.id,
    kind: o.kind as PushEvent['kind'],
    taskId: o.taskId,
    title: o.title,
    body: o.body,
    at: o.at,
  };
  if (o.runId !== undefined) event.runId = o.runId;
  if (o.chatId !== undefined) event.chatId = o.chatId;
  if (o.automationId !== undefined) event.automationId = o.automationId;
  return event;
}

export function openBlob(key: Uint8Array, deviceId: string, blob: unknown): OpenResult {
  if (key.length !== 32) return fail('key');
  if (typeof blob !== 'object' || blob === null || Array.isArray(blob)) return fail('malformed');
  const b = blob as Record<string, unknown>;
  if (typeof b.v !== 'number' || typeof b.n !== 'string' || typeof b.c !== 'string') return fail('malformed');
  if (b.v !== 1) return fail('version');

  let nonce: Uint8Array;
  let ct: Uint8Array;
  try {
    nonce = base64UrlDecode(b.n);
    ct = base64UrlDecode(b.c);
  } catch {
    return fail('malformed');
  }
  if (nonce.length !== 12 || ct.length < 18) return fail('malformed');

  let plain: Uint8Array;
  try {
    const aad = new TextEncoder().encode(AAD_PREFIX + deviceId);
    plain = chacha20poly1305(key, nonce, aad).decrypt(ct);
  } catch {
    return fail('auth');
  }

  if (plain.length < 2) return fail('malformed');
  const n = (plain[0] << 8) | plain[1];
  if (2 + n > plain.length) return fail('malformed');
  for (let i = 2 + n; i < plain.length; i++) {
    if (plain[i] !== 0) return fail('malformed');
  }

  let parsed: unknown;
  try {
    const text = new TextDecoder('utf-8', { fatal: true }).decode(plain.subarray(2, 2 + n));
    parsed = JSON.parse(text);
  } catch {
    return fail('malformed');
  }
  const event = toEvent(parsed);
  return event === null ? fail('malformed') : { ok: true, event };
}

export function openWithAnyKey(
  keys: readonly PushKeyEntry[],
  blob: unknown,
): { ok: true; event: PushEvent; deviceId: string } | { ok: false; error: OpenFailure } {
  let last: OpenFailure = 'key';
  for (const entry of keys) {
    const r = openBlob(entry.key, entry.deviceId, blob);
    if (r.ok) return { ok: true, event: r.event, deviceId: entry.deviceId };
    last = r.error;
    // These fail identically for every key; no point trying the rest.
    if (last === 'version' || last === 'malformed') break;
  }
  return { ok: false, error: last };
}

export function notificationFor(
  result: ReturnType<typeof openWithAnyKey>,
  nowSeconds: number,
): { title: string; body: string; data: { eventId: string; taskId: number; chatId?: number; automationId?: number } | null } | null {
  if (!result.ok) return { ...GENERIC_NOTIFICATION, data: null };
  const { event } = result;
  if (nowSeconds - event.at > MAX_AGE_SECONDS) return null;
  return {
    title: event.title,
    body: event.body,
    data: {
      eventId: event.id,
      taskId: event.taskId,
      ...(event.chatId !== undefined ? { chatId: event.chatId } : {}),
      ...(event.automationId !== undefined && event.automationId > 0 ? { automationId: event.automationId } : {}),
    },
  };
}
