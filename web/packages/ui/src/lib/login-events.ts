import type { LoginEvent } from "@/lib/types";

// ADR-0033's login contract: parsing and human copy for the events
// account.oauthStart streams and the errors it can fail with.

const KNOWN_LOGIN_ERROR_CODES = new Set([
  "denied",
  "timeout",
  "state_mismatch",
  "login_bad_redirect",
  "login_attempts_exceeded",
  "login_already_completed",
  "login_wrong_method",
  "login_unknown",
  "login_in_progress",
  "login_bad_method",
  "login_unknown_account",
  "login_not_oauth_account",
  "identity_mismatch",
  "identity_in_use",
  "identity_unverified",
  "device_unsupported",
  "device_expired",
  "device_failed",
]);

const LOGIN_ERROR_COPY: Record<string, string> = {
  denied: "The sign-in was cancelled or denied.",
  timeout: "The sign-in timed out. Start again.",
  device_expired: "The device code expired. Start again.",
  device_unsupported: "Device-code sign-in isn't available for this account. Use the browser sign-in instead.",
  device_failed: "Device-code sign-in failed. Try again, or use the browser sign-in.",
  state_mismatch: "That address doesn't match this sign-in. Copy it again from the browser's address bar.",
  login_bad_redirect: "That doesn't look like the sign-in redirect address. Copy the whole address from the browser's address bar.",
  login_attempts_exceeded: "Too many attempts. Cancel and start the sign-in again.",
  login_unknown: "This sign-in is no longer active. Start again.",
  login_already_completed: "This sign-in is no longer active. Start again.",
  login_in_progress: "A sign-in for this provider is already in progress.",
  login_wrong_method: "This sign-in can't be completed that way.",
  login_bad_method: "That sign-in method isn't available.",
  identity_mismatch: "You signed in to a different account than this row. Connect it as a new account instead.",
  identity_in_use: "That account is already connected as another row.",
  identity_unverified: "Couldn't confirm which account you signed in to, so this row was left unchanged.",
  login_not_oauth_account: "This account uses an API key, so it can't be signed in again.",
  login_unknown_account: "That account no longer exists.",
};

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value !== "";
}

/** An http(s) URL (the daemon's own redirects are always loopback http). */
function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export function parseLoginEvent(event: string, params: unknown): LoginEvent | null {
  if (typeof params !== "object" || params === null) return null;
  const p = params as Record<string, unknown>;
  if (event === "authorizeUrl") {
    if (!isNonEmptyString(p.loginId) || !isNonEmptyString(p.url) || !isHttpUrl(p.url)) return null;
    return { kind: "authorizeUrl", loginId: p.loginId, url: p.url, pasteOnly: p.pasteOnly === true };
  }
  if (event === "deviceCode") {
    if (
      !isNonEmptyString(p.loginId) ||
      !isNonEmptyString(p.verificationUrl) ||
      !isHttpUrl(p.verificationUrl) ||
      !isNonEmptyString(p.userCode) ||
      !isNonEmptyString(p.expiresAt) ||
      Number.isNaN(Date.parse(p.expiresAt)) ||
      typeof p.intervalSeconds !== "number" ||
      !Number.isFinite(p.intervalSeconds) ||
      p.intervalSeconds <= 0
    ) {
      return null;
    }
    return {
      kind: "deviceCode",
      loginId: p.loginId,
      verificationUrl: p.verificationUrl,
      userCode: p.userCode,
      expiresAt: p.expiresAt,
      intervalSeconds: p.intervalSeconds,
    };
  }
  return null;
}

export function parseLoginError(err: unknown): { code: string; detail: string } {
  const message = err instanceof Error ? err.message : String(err);
  let rest = message;
  for (const prefix of ["account.oauthStart: ", "account.oauthComplete: "]) {
    if (rest.startsWith(prefix)) {
      rest = rest.slice(prefix.length);
      break;
    }
  }
  // Legacy pre-contract message from an older daemon.
  if (rest.includes('is already in progress') && rest.startsWith("a login for provider ")) {
    return { code: "login_in_progress", detail: "" };
  }
  const match = /^([a-z][a-z_]*): (.*)$/s.exec(rest);
  if (match && KNOWN_LOGIN_ERROR_CODES.has(match[1])) {
    return { code: match[1], detail: match[2] };
  }
  return { code: "unknown", detail: rest };
}

export function loginErrorMessage(code: string, detail: string): string {
  return LOGIN_ERROR_COPY[code] ?? (detail || "Sign-in failed.");
}

export function isSoftLoginError(code: string): boolean {
  return code === "denied" || code === "device_expired" || code === "timeout";
}
