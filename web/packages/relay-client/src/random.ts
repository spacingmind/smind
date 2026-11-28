// random.ts is the one place the relay client gets random bytes from: key
// material (the E2EE client ephemeral, the Ed25519 device key), session ids,
// admission nonces and keepalive probe nonces.
//
// React Native, Hermes and Expo's runtime layer do not provide a global
// `crypto.getRandomValues`, so nothing here reads one. The app installs a
// native CSPRNG once at startup (platformRandom.ts, expo-crypto); node-side
// tests install node's. With no source installed, or a source that throws or
// returns a degenerate result, randomBytes throws: a missing CSPRNG must stop
// the connection, never silently weaken a key. There is no Math.random
// fallback anywhere in this path.
//
// Kept free of any native import so node-side tests can load it.

/** Fills the whole array with cryptographically secure random bytes, or throws. */
export type RandomFill = (bytes: Uint8Array) => void;

/** The largest request: expo-crypto's own limit, far above anything the relay needs (32 bytes). */
export const MAX_RANDOM_BYTES = 1024;

export class CsprngUnavailableError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(`no secure random source: ${message}`, options);
    this.name = 'CsprngUnavailableError';
  }
}

let source: RandomFill | null = null;

/** Installs the process-wide CSPRNG (once, at startup; tests install their own). `null` uninstalls it (tests of the fail-closed path). */
export function installRandomSource(fill: RandomFill | null): void {
  source = fill;
}

/** `n` secure random bytes. Throws CsprngUnavailableError rather than ever returning weak bytes. */
export function randomBytes(n: number): Uint8Array {
  if (!Number.isInteger(n) || n < 1 || n > MAX_RANDOM_BYTES) {
    throw new RangeError(`randomBytes(${n}): want an integer from 1 to ${MAX_RANDOM_BYTES}`);
  }
  if (!source) {
    throw new CsprngUnavailableError('none installed (platformRandom.ts must be imported at startup)');
  }
  const out = new Uint8Array(n);
  try {
    source(out);
  } catch (e) {
    throw new CsprngUnavailableError(e instanceof Error ? e.message : String(e), { cause: e });
  }
  // 16 or more bytes that are all zero is a source that did nothing (probability 2^-128 otherwise).
  if (n >= 16 && out.every((b) => b === 0)) {
    throw new CsprngUnavailableError('the source returned only zero bytes');
  }
  return out;
}
