import { WsClient, daemonWsBaseUrl, daemonWsUrl, sessionWsProtocols } from "@/lib/ws-client";
import {
  LoginRequiredError,
  clearSession,
  fetchAuthRequired,
  readSession,
  type LoginReason,
  type Session,
} from "@/lib/auth";

/**
 * Fetches the daemon's current auth token from the same-origin GET
 * /api/token endpoint. The daemon only hands it to a true loopback caller,
 * so the page's own JS can read it there, letting the web UI open /ws
 * without the user pasting a token in manually. Any other caller (a browser
 * on another machine) gets 403, which means "sign in with the password
 * instead" -- surfaced as LoginRequiredError.
 */
export async function fetchToken(): Promise<string> {
  const res = await fetch("/api/token");
  if (res.status === 403) {
    throw new LoginRequiredError("required");
  }
  if (!res.ok) {
    throw new Error(`/api/token: unexpected status ${res.status}`);
  }
  const body = (await res.json()) as { token: string };
  return body.token;
}

/**
 * Dials /ws with a password-login session token (as a subprotocol, never in
 * the URL). A browser WebSocket doesn't expose why a handshake failed, so a
 * failure is classified by probing /healthz: if the daemon answers, it was
 * reachable and refused the session (invalid or expired server-side), so the
 * session is dropped and the caller falls back to the login form; if the
 * probe fails too, the daemon is simply down and the session is kept for the
 * next attempt.
 */
async function connectWithSession(session: Session): Promise<WsClient | LoginReason> {
  try {
    return await WsClient.connect(daemonWsBaseUrl(), sessionWsProtocols(session.token));
  } catch (err) {
    try {
      await fetchAuthRequired();
    } catch {
      throw err;
    }
    clearSession();
    return "rejected";
  }
}

/**
 * Opens a ready-to-use /ws connection with whichever credential applies:
 *   1. a stored, unexpired session token (password login) -- used directly;
 *   2. otherwise the loopback daemon token from /api/token;
 *   3. otherwise (403, or the daemon reports authRequired) throws
 *      LoginRequiredError so the app shows the login form.
 * A refused session falls through to step 2 -- a loopback page with a stale
 * session still just works -- and only ends at the login form if that fails
 * too. Callers (including the reconnect loop) treat LoginRequiredError as
 * "stop retrying", never as a transient failure.
 */
export async function connectDaemon(): Promise<WsClient> {
  const { session, expired } = readSession();
  let reason: LoginReason = expired ? "expired" : "required";
  if (session) {
    const outcome = await connectWithSession(session);
    if (typeof outcome !== "string") return outcome;
    reason = outcome;
  }

  let token: string;
  try {
    token = await fetchToken();
  } catch (err) {
    if (err instanceof LoginRequiredError) throw new LoginRequiredError(reason);
    // Not a 403: if the daemon says it needs a password, ask for one rather
    // than reporting an unreachable daemon.
    let authRequired = false;
    try {
      authRequired = await fetchAuthRequired();
    } catch {
      // Unreachable: surface the original error.
    }
    if (authRequired) throw new LoginRequiredError(reason);
    throw err;
  }
  return WsClient.connect(daemonWsUrl(token));
}
