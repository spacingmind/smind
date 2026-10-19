import { describe, expect, it } from "vitest";
import { isSoftLoginError, loginErrorMessage, parseLoginError, parseLoginEvent } from "@/lib/login-events";

describe("parseLoginEvent", () => {
  it("accepts authorizeUrl and deviceCode", () => {
    expect(
      parseLoginEvent("authorizeUrl", { loginId: "l1", url: "https://claude.ai/oauth/authorize?x=1", pasteOnly: true }),
    ).toEqual({ kind: "authorizeUrl", loginId: "l1", url: "https://claude.ai/oauth/authorize?x=1", pasteOnly: true });
    expect(parseLoginEvent("authorizeUrl", { loginId: "l1", url: "https://x" })).toEqual({
      kind: "authorizeUrl",
      loginId: "l1",
      url: "https://x",
      pasteOnly: false,
    });
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l2",
        verificationUrl: "https://github.com/login/device",
        userCode: "ABCD-1234",
        expiresAt: "2026-01-01T00:15:00Z",
        intervalSeconds: 5,
      }),
    ).toEqual({
      kind: "deviceCode",
      loginId: "l2",
      verificationUrl: "https://github.com/login/device",
      userCode: "ABCD-1234",
      expiresAt: "2026-01-01T00:15:00Z",
      intervalSeconds: 5,
    });
  });

  it("rejects malformed events", () => {
    expect(parseLoginEvent("chunk", { text: "hi" })).toBeNull();
    expect(parseLoginEvent("authorizeUrl", null)).toBeNull();
    expect(parseLoginEvent("authorizeUrl", "nope")).toBeNull();
    expect(parseLoginEvent("authorizeUrl", { loginId: "", url: "https://x" })).toBeNull();
    expect(parseLoginEvent("authorizeUrl", { loginId: "l1", url: "" })).toBeNull();
    expect(parseLoginEvent("authorizeUrl", { loginId: "l1" })).toBeNull();
    expect(parseLoginEvent("deviceCode", { loginId: "l2", verificationUrl: "https://x", userCode: "A", expiresAt: "" })).toBeNull();
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l2",
        verificationUrl: "https://x",
        userCode: "A",
        expiresAt: "2026-01-01T00:15:00Z",
        intervalSeconds: 0,
      }),
    ).toBeNull();
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l2",
        verificationUrl: "https://x",
        userCode: "A",
        expiresAt: "2026-01-01T00:15:00Z",
        intervalSeconds: Number.POSITIVE_INFINITY,
      }),
    ).toBeNull();
  });
});

describe("parseLoginError", () => {
  it("splits code and detail", () => {
    expect(parseLoginError(new Error('account.oauthStart: denied: user said no'))).toEqual({
      code: "denied",
      detail: "user said no",
    });
    expect(parseLoginError(new Error("account.oauthComplete: login_bad_redirect: pasted nonsense"))).toEqual({
      code: "login_bad_redirect",
      detail: "pasted nonsense",
    });
  });

  it("handles unknown and legacy messages", () => {
    expect(parseLoginError(new Error("account.oauthStart: not_a_known_code: whatever"))).toEqual({
      code: "unknown",
      detail: "not_a_known_code: whatever",
    });
    expect(parseLoginError(new Error("something completely different"))).toEqual({
      code: "unknown",
      detail: "something completely different",
    });
    expect(parseLoginError('a login for provider "anthropic" is already in progress')).toEqual({
      code: "login_in_progress",
      detail: "",
    });
    expect(parseLoginError(42)).toEqual({ code: "unknown", detail: "42" });
  });
});

describe("loginErrorMessage", () => {
  it("has copy for every known code", () => {
    expect(loginErrorMessage("denied", "")).toBe("The sign-in was cancelled or denied.");
    expect(loginErrorMessage("timeout", "")).toBe("The sign-in timed out. Start again.");
    expect(loginErrorMessage("device_expired", "")).toBe("The device code expired. Start again.");
    expect(loginErrorMessage("device_unsupported", "")).toBe(
      "Device-code sign-in isn't available for this account. Use the browser sign-in instead.",
    );
    expect(loginErrorMessage("device_failed", "")).toBe("Device-code sign-in failed. Try again, or use the browser sign-in.");
    expect(loginErrorMessage("state_mismatch", "")).toBe(
      "That address doesn't match this sign-in. Copy it again from the browser's address bar.",
    );
    expect(loginErrorMessage("login_bad_redirect", "")).toBe(
      "That doesn't look like the sign-in redirect address. Copy the whole address from the browser's address bar.",
    );
    expect(loginErrorMessage("login_attempts_exceeded", "")).toBe("Too many attempts. Cancel and start the sign-in again.");
    expect(loginErrorMessage("login_unknown", "")).toBe("This sign-in is no longer active. Start again.");
    expect(loginErrorMessage("login_already_completed", "")).toBe("This sign-in is no longer active. Start again.");
    expect(loginErrorMessage("login_in_progress", "")).toBe("A sign-in for this provider is already in progress.");
    expect(loginErrorMessage("login_wrong_method", "")).toBe("This sign-in can't be completed that way.");
    expect(loginErrorMessage("login_bad_method", "")).toBe("That sign-in method isn't available.");
    expect(loginErrorMessage("identity_mismatch", "")).toBe(
      "You signed in to a different account than this row. Connect it as a new account instead.",
    );
    expect(loginErrorMessage("identity_in_use", "")).toBe("That account is already connected as another row.");
    expect(loginErrorMessage("identity_unverified", "")).toBe(
      "Couldn't confirm which account you signed in to, so this row was left unchanged.",
    );
    expect(loginErrorMessage("login_not_oauth_account", "")).toBe("This account uses an API key, so it can't be signed in again.");
    expect(loginErrorMessage("login_unknown_account", "")).toBe("That account no longer exists.");
    expect(loginErrorMessage("mystery", "")).toBe("Sign-in failed.");
    expect(loginErrorMessage("mystery", "weird detail")).toBe("weird detail");
  });
});

describe("isSoftLoginError", () => {
  it("only for denied, device_expired and timeout", () => {
    expect(isSoftLoginError("denied")).toBe(true);
    expect(isSoftLoginError("device_expired")).toBe(true);
    expect(isSoftLoginError("timeout")).toBe(true);
    expect(isSoftLoginError("identity_mismatch")).toBe(false);
    expect(isSoftLoginError("unknown")).toBe(false);
  });

  it("parseLoginEvent rejects non-http urls and bad expiry", () => {
    expect(parseLoginEvent("authorizeUrl", { loginId: "l", url: "javascript:alert(1)" })).toBeNull();
    expect(parseLoginEvent("authorizeUrl", { loginId: "l", url: "ftp://example.com" })).toBeNull();
    expect(parseLoginEvent("authorizeUrl", { loginId: "l", url: "not a url" })).toBeNull();
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l",
        verificationUrl: "javascript:alert(1)",
        userCode: "X",
        expiresAt: "2099-01-01T00:00:00Z",
        intervalSeconds: 5,
      }),
    ).toBeNull();
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l",
        verificationUrl: "https://example.com/device",
        userCode: "X",
        expiresAt: "not a date",
        intervalSeconds: 5,
      }),
    ).toBeNull();
    // A zero/negative interval is rejected just as before.
    expect(
      parseLoginEvent("deviceCode", {
        loginId: "l",
        verificationUrl: "https://example.com/device",
        userCode: "X",
        expiresAt: "2099-01-01T00:00:00Z",
        intervalSeconds: 0,
      }),
    ).toBeNull();
    // The valid shapes still parse.
    expect(parseLoginEvent("authorizeUrl", { loginId: "l", url: "https://a" })).toMatchObject({ kind: "authorizeUrl" });
  });
});
