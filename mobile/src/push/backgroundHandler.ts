// Pure logic of the Android background push task (ADR-0040 section 1): turns the
// untrusted task payload into a local notification. No react-native or expo
// imports; the native glue is src/push/native/backgroundTask.ts.

import { notificationFor, openWithAnyKey, type PushKeyEntry } from './open';

export const PUSH_TASK_NAME = 'smind-push-notification';
/** The Android notification channel the foreground code creates for attention pushes. */
export const ATTENTION_CHANNEL = 'attention';

export interface PresentContent {
  identifier?: string;
  title: string;
  body: string;
  data: Record<string, unknown>;
}

export interface BackgroundDeps {
  keys: { all(): Promise<PushKeyEntry[]> };
  nowSeconds(): number;
  present(content: PresentContent): Promise<void>;
}

export type BackgroundOutcome = 'shown' | 'generic' | 'dropped' | 'ignored';

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function isBlob(v: unknown): v is Record<string, unknown> {
  return isObject(v) && typeof v.v === 'number' && typeof v.n === 'string' && typeof v.c === 'string';
}

function parseJson(s: unknown): unknown {
  if (typeof s !== 'string') return undefined;
  try {
    return JSON.parse(s);
  } catch {
    return undefined;
  }
}

/**
 * Finds the sealed blob in a task payload. The exact payload shape is not yet
 * verified on a real device (ADR-0040 Consequences), so several shapes are
 * accepted. Never throws; null means "not one of ours".
 */
export function blobFromTaskPayload(payload: unknown): unknown | null {
  if (!isObject(payload)) return null;
  const data = payload.data;
  if (isObject(data)) {
    const parsed = parseJson(data.dataString);
    if (isBlob(parsed)) return parsed;
    if (isObject(parsed)) {
      const inner = parseJson(parsed.body);
      if (isBlob(inner)) return inner;
    }
    if (isBlob(data)) return data;
  }
  if (isBlob(payload)) return payload;
  return null;
}

export async function handleBackgroundPush(payload: unknown, deps: BackgroundDeps): Promise<BackgroundOutcome> {
  const blob = blobFromTaskPayload(payload);
  if (blob === null) return 'ignored';

  let keys: PushKeyEntry[] = [];
  try {
    keys = await deps.keys.all();
  } catch {
    keys = [];
  }

  const result = openWithAnyKey(keys, blob);
  const content = notificationFor(result, deps.nowSeconds());
  if (content === null) return 'dropped';

  if (result.ok && content.data !== null) {
    await deps.present({
      identifier: content.data.eventId,
      title: content.title,
      body: content.body,
      data: content.data,
    });
    return 'shown';
  }
  await deps.present({ title: content.title, body: content.body, data: {} });
  return 'generic';
}
