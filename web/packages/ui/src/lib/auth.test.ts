import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  LoginError,
  SESSION_STORAGE_KEY,
  clearSession,
  fetchAuthRequired,
  loginWithPassword,
  readSession,
  saveSession,
} from "@/lib/auth";

const FUTURE = () => new Date(Date.now() + 24 * 3600_000).toISOString();

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });
}

beforeEach(() => {
  clearSession();
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
  clearSession();
});

describe("session storage", () => {
  it("keeps the session token out of localStorage after the chosen lifetime", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    saveSession({ token: "tok-abc", expiresAt: "2026-01-02T00:00:00Z" });

    // Written to sessionStorage only, never localStorage.
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.getItem(SESSION_STORAGE_KEY)).toContain("tok-abc");
    expect(setItem.mock.instances.every((i) => i === window.sessionStorage)).toBe(true);

    expect(readSession().session?.token).toBe("tok-abc");

    // Past expiresAt it is dropped from memory and sessionStorage alike.
    vi.setSystemTime(new Date("2026-01-02T00:00:01Z"));
    expect(readSession()).toEqual({ session: null, expired: true });
    expect(window.sessionStorage.getItem(SESSION_STORAGE_KEY)).toBeNull();
    expect(window.localStorage.length).toBe(0);
  });

  it("survives a reload (re-reads sessionStorage) while unexpired", () => {
    window.sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify({ token: "t", expiresAt: FUTURE() }));
    expect(readSession().session?.token).toBe("t");
  });

  it("treats malformed stored data as no session", () => {
    window.sessionStorage.setItem(SESSION_STORAGE_KEY, "{not json");
    expect(readSession()).toEqual({ session: null, expired: false });
  });
});

describe("loginWithPassword", () => {
  it("saves the session and never stores the password", async () => {
    const expiresAt = FUTURE();
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(200, { token: "sess-1", expiresAt }));
    vi.stubGlobal("fetch", fetchMock);
    const log = vi.spyOn(console, "log");

    const session = await loginWithPassword("hunter2-secret");

    expect(session).toEqual({ token: "sess-1", expiresAt });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/login");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body as string)).toEqual({ password: "hunter2-secret" });

    const everywhere = JSON.stringify({ ...window.localStorage }) + JSON.stringify({ ...window.sessionStorage });
    expect(everywhere).not.toContain("hunter2-secret");
    expect(log).not.toHaveBeenCalled();
  });

  it("maps 401 to wrong-password and saves nothing", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(401, { error: "bad" })));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "wrong-password" } });
    expect(readSession().session).toBeNull();
  });

  it("maps 429 to rate-limited using retryAfterSeconds, falling back to Retry-After", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(429, { error: "slow", retryAfterSeconds: 12 })));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "rate-limited", retryAfterSeconds: 12 } });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(429, { error: "slow" }, { "Retry-After": "7" })));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "rate-limited", retryAfterSeconds: 7 } });
  });

  it("maps 403 to disabled, network errors to network, other statuses to server", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(403, { error: "off" })));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "disabled" } });

    vi.stubGlobal("fetch", vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch")));
    await expect(loginWithPassword("x")).rejects.toBeInstanceOf(LoginError);
    vi.stubGlobal("fetch", vi.fn().mockRejectedValueOnce(new TypeError("Failed to fetch")));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "network" } });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(500, {})));
    await expect(loginWithPassword("x")).rejects.toMatchObject({ failure: { kind: "server", status: 500 } });
  });
});

describe("fetchAuthRequired", () => {
  it("reads authRequired and treats a missing field as false", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(200, { status: "ok", authRequired: true })));
    await expect(fetchAuthRequired()).resolves.toBe(true);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(jsonResponse(200, { status: "ok" })));
    await expect(fetchAuthRequired()).resolves.toBe(false);
  });
});
