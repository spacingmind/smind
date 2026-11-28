// daemonPasswords.ts holds the daemon password a person typed this app run
// (ADR-0031 P13c), in JS memory only: never in the secure store, a keychain,
// AsyncStorage, a log, a URL, an error message, React state or analytics. It
// lives in a closure, so serialising or logging the store (or whatever holds
// it) cannot reveal it, and it is gone when the app process ends.
//
// A password is only ever sent to the daemon it was typed for: it is keyed by
// the relay address, the relay host id AND the daemon's public key, so a
// pairing offer with the same relay host id but a different daemon key starts
// with nothing.

import { PairingOffer } from './pairing';

/** The daemon refuses a hello password longer than this many UTF-8 bytes (as incorrect_password, counted as a wrong attempt). */
export const MAX_PASSWORD_BYTES = 128;

export interface DaemonPasswords {
  /** The password typed for this daemon this run, if any. */
  get(key: string): string | undefined;
  set(key: string, password: string): void;
  /**
   * Forgets the password only if it is still exactly `password`: the one the
   * daemon just rejected, not a newer one the user typed while the reply was
   * in flight.
   */
  forgetIfEqual(key: string, password: string): void;
}

/** Why a typed password cannot be submitted at all (empty, or longer than the daemon accepts), or null when it can. */
export function passwordProblem(password: string): string | null {
  if (password.length === 0) return 'Enter the daemon password.';
  if (new TextEncoder().encode(password).length > MAX_PASSWORD_BYTES) return `The daemon password is at most ${MAX_PASSWORD_BYTES} bytes.`;
  return null;
}

/** The key under which a password is held: relay address + relay host id + daemon public key (all from the pairing offer). */
export function passwordKey(offer: PairingOffer): string {
  const pub = Array.from(offer.publicKey)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
  return `${offer.relay}|${offer.workspaceId}|${pub}`;
}

export function createDaemonPasswords(): DaemonPasswords {
  const held = new Map<string, string>();
  return {
    get: (key) => held.get(key),
    set: (key, password) => {
      held.set(key, password);
    },
    forgetIfEqual: (key, password) => {
      if (held.get(key) === password) held.delete(key);
    },
  };
}

/** The app-wide store: one per process, memory only. */
export const appDaemonPasswords: DaemonPasswords = createDaemonPasswords();
