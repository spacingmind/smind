// connectionActions.ts holds the pure rules of ConnectionProvider so they are
// unit-tested without React Native.

/**
 * Forget the device identity, then leave the session. If the old key cannot be
 * removed, stay where we are: leaving would reconnect with the same (revoked)
 * identity. Returns whether the session was left.
 */
export async function performResetIdentity(reset: () => Promise<void>, disconnect: () => void): Promise<boolean> {
  try {
    await reset();
  } catch {
    return false;
  }
  disconnect();
  return true;
}

/**
 * A new session replaces the old one: a different previous connection is closed
 * (close() is idempotent), the same one is left alone. Returns the connection to keep.
 */
export function replaceConnection<T extends { close(): void }>(prev: T | null, next: T): T {
  if (prev !== null && prev !== next) prev.close();
  return next;
}

/** Leaving the session closes the current connection (if any) and keeps none. */
export function dropConnection(cur: { close(): void } | null): null {
  cur?.close();
  return null;
}
