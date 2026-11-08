/** The initial-connect failure as the shell shows it: one sentence for the home notice, the raw error kept for a tooltip. */
export interface ConnectCause {
  copy: string;
  raw: string;
}

/** A connect that never settled within this long is reported as "no answer" (App.tsx's timer). */
export const CONNECT_TIMEOUT_MS = 8_000;

const NETWORK_ERROR = /failed to fetch|networkerror|load failed|failed to connect|econnrefused|connection refused/i;

/**
 * Maps a transport error to human copy: a refused or failed connection says the daemon is not running or
 * not reachable (naming the address when it is a web origin), a connect that hung says it did not answer,
 * anything else (an HTTP status, say) is quoted so it is not hidden.
 */
export function describeConnectError(raw: string | null, timedOut: boolean, origin: string = window.location.origin): ConnectCause | null {
  if (timedOut) {
    return { copy: `The daemon didn't answer within ${CONNECT_TIMEOUT_MS / 1000} s.`, raw: raw ?? "connect timed out" };
  }
  if (raw === null) return null;
  if (NETWORK_ERROR.test(raw)) {
    const at = /^https?:/.test(origin) ? ` at ${origin}` : "";
    return { copy: `The daemon isn't running or isn't reachable${at}.`, raw };
  }
  return { copy: `The daemon answered with an error: ${raw}`, raw };
}
