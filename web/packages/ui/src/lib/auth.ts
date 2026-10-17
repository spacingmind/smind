// Password-login credentials for a browser that reaches the daemon over the
// LAN (the loopback path -- GET /api/token -- needs none of this).
//
// Session-token storage policy
// ----------------------------
// A session token (256-bit, ~24 h life, minted by POST /api/login) is held
// in module memory and mirrored to **sessionStorage** only, together with its
// `expiresAt`:
//   - sessionStorage is per-tab and dies with the tab, so a reload keeps you
//     signed in but a closed tab, another tab, or a shared machine tomorrow
//     does not inherit the credential;
//   - it is NEVER written to localStorage (persists across browser restarts
//     and is shared across tabs, which would turn a 24 h token into a
//     long-lived one) and never put in a URL (the /ws handshake carries it as
//     a subprotocol -- see ws-client.ts);
//   - it is dropped as soon as it is read past `expiresAt`, and whenever the
//     daemon rejects it.
// The *password* is not stored anywhere at all: LoginScreen reads it from an
// uncontrolled input, hands it to `loginWithPassword`, and clears the field.

/** The one sessionStorage key holding `{ token, expiresAt }` (JSON). */
export const SESSION_STORAGE_KEY = "smind:session";

export interface Session {
  token: string;
  /** RFC3339, as returned by POST /api/login. */
  expiresAt: string;
}

/** Why a login is needed: nothing usable stored, a stored one lapsed, or the daemon refused it. */
export type LoginReason = "required" | "expired" | "rejected";

/** Thrown by connectDaemon when the app must show the login form instead of connecting. */
export class LoginRequiredError extends Error {
  readonly reason: LoginReason;
  constructor(reason: LoginReason = "required") {
    super(`login required (${reason})`);
    this.name = "LoginRequiredError";
    this.reason = reason;
  }
}

let memorySession: Session | null = null;

function isUsable(s: Session | null, now: number): s is Session {
  if (!s || typeof s.token !== "string" || s.token === "") return false;
  const exp = Date.parse(s.expiresAt);
  return Number.isFinite(exp) && exp > now;
}

function readStored(): Session | null {
  try {
    const raw = window.sessionStorage.getItem(SESSION_STORAGE_KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<Session> | null;
    if (v && typeof v.token === "string" && typeof v.expiresAt === "string") {
      return { token: v.token, expiresAt: v.expiresAt };
    }
  } catch {
    // Unavailable or malformed storage reads as "no session".
  }
  return null;
}

/**
 * The stored session if there is one and it hasn't expired. `expired` is true
 * when a session existed but had lapsed (it is dropped as a side effect), so
 * the login form can say "session expired" rather than a bare prompt.
 */
export function readSession(now: number = Date.now()): { session: Session | null; expired: boolean } {
  const candidate = memorySession ?? readStored();
  if (!candidate) return { session: null, expired: false };
  if (isUsable(candidate, now)) {
    memorySession = candidate;
    return { session: candidate, expired: false };
  }
  clearSession();
  return { session: null, expired: true };
}

export function saveSession(session: Session): void {
  memorySession = session;
  try {
    window.sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(session));
  } catch {
    // Storage unavailable: the in-memory copy still serves this page load.
  }
}

export function clearSession(): void {
  memorySession = null;
  try {
    window.sessionStorage.removeItem(SESSION_STORAGE_KEY);
  } catch {
    // Nothing to clean up if storage is unavailable.
  }
}

/** Why a password login failed -- LoginScreen maps each to its own message. */
export type LoginFailure =
  | { kind: "wrong-password" }
  | { kind: "rate-limited"; retryAfterSeconds: number }
  | { kind: "disabled" }
  | { kind: "network" }
  | { kind: "server"; status: number };

export class LoginError extends Error {
  readonly failure: LoginFailure;
  constructor(failure: LoginFailure) {
    super(`login failed: ${failure.kind}`);
    this.name = "LoginError";
    this.failure = failure;
  }
}

/** Fallback wait when a 429 carries neither retryAfterSeconds nor a Retry-After header. */
const DEFAULT_RETRY_AFTER_SECONDS = 30;

async function readJson(res: Response): Promise<Record<string, unknown>> {
  try {
    const v: unknown = await res.json();
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/**
 * POST /api/login. On success the session is saved (memory + sessionStorage)
 * and returned. The password is only ever a local argument here: it is not
 * logged, cached or retained after the request is built.
 */
export async function loginWithPassword(password: string): Promise<Session> {
  let res: Response;
  try {
    res = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
  } catch {
    throw new LoginError({ kind: "network" });
  }
  if (res.status === 401) throw new LoginError({ kind: "wrong-password" });
  if (res.status === 403) throw new LoginError({ kind: "disabled" });
  if (res.status === 429) {
    const body = await readJson(res);
    const header = Number(res.headers.get("Retry-After"));
    const fromBody = Number(body.retryAfterSeconds);
    const seconds = [fromBody, header].find((n) => Number.isFinite(n) && n > 0) ?? DEFAULT_RETRY_AFTER_SECONDS;
    throw new LoginError({ kind: "rate-limited", retryAfterSeconds: Math.ceil(seconds) });
  }
  if (!res.ok) throw new LoginError({ kind: "server", status: res.status });
  const body = await readJson(res);
  if (typeof body.token !== "string" || typeof body.expiresAt !== "string") {
    throw new LoginError({ kind: "server", status: res.status });
  }
  const session: Session = { token: body.token, expiresAt: body.expiresAt };
  saveSession(session);
  return session;
}

/** GET /healthz's `authRequired` (a missing field -- an older daemon -- is false). Network failure rejects. */
export async function fetchAuthRequired(): Promise<boolean> {
  const res = await fetch("/healthz");
  if (!res.ok) return false;
  const body = await readJson(res);
  return body.authRequired === true;
}
