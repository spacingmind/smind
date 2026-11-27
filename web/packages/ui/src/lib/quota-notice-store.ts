import { quotaKeyResetMs } from "@/lib/ambient-quota";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** A dismissal outlives its window by at most this long, then the key is pruned. */
const QUOTA_NOTICE_PRUNE_AFTER_MS = 24 * 60 * 60 * 1000;

/** The stored list is bounded: the oldest dismissals are dropped past this size. */
const MAX_DISMISSED_QUOTA_NOTICES = 200;

function isStringArray(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === "string");
}

/**
 * `useSyncExternalStore` needs the snapshot to be referentially stable between
 * changes, so the cache holds one array until a write replaces it.
 */
let cache: string[] | null = null;

const listeners = new Set<() => void>();

function notify(): void {
  for (const listener of listeners) listener();
}

export function readDismissedQuotaNotices(): string[] {
  if (cache === null) cache = readStored(STORAGE_KEYS.dismissedQuotaNotices, [], isStringArray);
  return cache;
}

/** Remembers a dismissed notice: adds the key, prunes ended windows, keeps the list bounded. */
export function dismissQuotaNotice(key: string, now: number = Date.now()): void {
  const current = readDismissedQuotaNotices();
  const next = current.filter((entry) => {
    const reset = quotaKeyResetMs(entry);
    return reset !== null && reset >= now - QUOTA_NOTICE_PRUNE_AFTER_MS;
  });
  if (!next.includes(key)) next.push(key);
  while (next.length > MAX_DISMISSED_QUOTA_NOTICES) next.shift();
  writeStored(STORAGE_KEYS.dismissedQuotaNotices, next);
  cache = next;
  notify();
}

/**
 * Subscribes to dismissal changes, including from another tab: the first
 * subscriber also listens for `storage`, clears the cache and notifies, so a
 * dismissal elsewhere is seen here.
 */
export function subscribeDismissedQuotaNotices(cb: () => void): () => void {
  listeners.add(cb);
  const onStorage = (): void => {
    cache = null;
    notify();
  };
  window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(cb);
    window.removeEventListener("storage", onStorage);
  };
}

/** Returns the module to a pristine state between tests. */
export function __resetQuotaNoticeStoreForTests(): void {
  cache = null;
  listeners.clear();
}
