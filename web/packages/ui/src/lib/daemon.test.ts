import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { LoginRequiredError, SESSION_STORAGE_KEY, clearSession, readSession, saveSession } from "@/lib/auth";
import { connectDaemon } from "@/lib/daemon";
import { WsClient } from "@/lib/ws-client";

/** Records constructions and lets a test decide whether each handshake opens or fails. */
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  static behaviour: "open" | "error" = "open";
  readonly url: string;
  readonly protocols: string | string[] | undefined;
  private listeners = new Map<string, Array<() => void>>();

  constructor(url: string, protocols?: string | string[]) {
    this.url = url;
    this.protocols = protocols;
    MockWebSocket.instances.push(this);
    queueMicrotask(() => this.fire(MockWebSocket.behaviour));
  }

  addEventListener(type: string, fn: () => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  removeEventListener(type: string, fn: () => void): void {
    this.listeners.set(type, (this.listeners.get(type) ?? []).filter((f) => f !== fn));
  }
  close(): void {}
  send(): void {}
  private fire(type: string): void {
    for (const fn of this.listeners.get(type) ?? []) fn();
  }
}

type Route = (init?: RequestInit) => Response | Promise<Response>;

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function stubFetch(routes: Record<string, Route>) {
  const fn = vi.fn((url: string, init?: RequestInit) => {
    const route = routes[url];
    if (!route) return Promise.reject(new TypeError(`unrouted ${url}`));
    return Promise.resolve(route(init));
  });
  vi.stubGlobal("fetch", fn);
  return fn;
}

const FUTURE = () => new Date(Date.now() + 3600_000).toISOString();

beforeEach(() => {
  MockWebSocket.instances = [];
  MockWebSocket.behaviour = "open";
  vi.stubGlobal("WebSocket", MockWebSocket);
  clearSession();
  window.sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  clearSession();
});

describe("connectDaemon", () => {
  it("does not ask for a login on loopback: /api/token works and /ws keeps the ?token= URL", async () => {
    const fetchMock = stubFetch({ "/api/token": () => json(200, { token: "daemon-tok" }) });

    const client = await connectDaemon();

    expect(client).toBeInstanceOf(WsClient);
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0]!.url).toContain("/ws?token=daemon-tok");
    expect(MockWebSocket.instances[0]!.protocols).toBeUndefined();
    expect(fetchMock.mock.calls.map((c) => c[0])).toEqual(["/api/token"]);
  });

  it("requires a login when /api/token answers 403 and there is no session", async () => {
    stubFetch({ "/api/token": () => json(403, { error: "forbidden" }) });
    await expect(connectDaemon()).rejects.toBeInstanceOf(LoginRequiredError);
    expect(MockWebSocket.instances).toHaveLength(0);
  });

  it("requires a login when /api/token is unavailable but healthz says authRequired", async () => {
    stubFetch({
      "/api/token": () => json(404, {}),
      "/healthz": () => json(200, { status: "ok", authRequired: true }),
    });
    await expect(connectDaemon()).rejects.toBeInstanceOf(LoginRequiredError);
  });

  it("reports a plain failure (not a login) when the daemon is unreachable", async () => {
    stubFetch({});
    const err = await connectDaemon().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TypeError);
    expect(err).not.toBeInstanceOf(LoginRequiredError);
  });

  it("connects with a stored session via subprotocols, with the token out of the URL", async () => {
    saveSession({ token: "sess-xyz", expiresAt: FUTURE() });
    const fetchMock = stubFetch({});

    await connectDaemon();

    const ws = MockWebSocket.instances[0]!;
    expect(ws.url).not.toContain("sess-xyz");
    expect(ws.url).not.toContain("token=");
    expect(ws.protocols).toEqual(["smind", "smind.bearer.sess-xyz"]);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("an expired session shows the login again (reason: expired) and is dropped", async () => {
    window.sessionStorage.setItem(
      SESSION_STORAGE_KEY,
      JSON.stringify({ token: "old", expiresAt: new Date(Date.now() - 1000).toISOString() }),
    );
    stubFetch({ "/api/token": () => json(403, { error: "forbidden" }) });

    await expect(connectDaemon()).rejects.toMatchObject({ reason: "expired" });
    expect(MockWebSocket.instances).toHaveLength(0);
    expect(window.sessionStorage.getItem(SESSION_STORAGE_KEY)).toBeNull();
  });

  it("a rejected session handshake clears the session and shows the login again", async () => {
    saveSession({ token: "revoked", expiresAt: FUTURE() });
    MockWebSocket.behaviour = "error";
    stubFetch({
      "/healthz": () => json(200, { status: "ok", authRequired: true }),
      "/api/token": () => json(403, { error: "forbidden" }),
    });

    await expect(connectDaemon()).rejects.toMatchObject({ name: "LoginRequiredError", reason: "rejected" });
    expect(readSession().session).toBeNull();
  });

  it("a failed session handshake with the daemon down keeps the session and is not a login", async () => {
    saveSession({ token: "keep-me", expiresAt: FUTURE() });
    MockWebSocket.behaviour = "error";
    stubFetch({});

    const err = await connectDaemon().catch((e: unknown) => e);
    expect(err).not.toBeInstanceOf(LoginRequiredError);
    expect(readSession().session?.token).toBe("keep-me");
  });
});
