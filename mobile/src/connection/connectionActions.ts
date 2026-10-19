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
