import { act } from "react";
import { cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { OAuthStartResult } from "@/lib/types";

// `isDesktop` is a module-load constant, so the platform is mocked per
// scenario and the hook imported fresh (accounts-dialog.desktop.test.tsx's
// pattern); every test goes through the same setup for consistency.

const PASTED_URL = "http://127.0.0.1:47891/callback?state=abc&code=SECRET";

function accountResult(): OAuthStartResult {
  return {
    id: 7,
    provider: "anthropic",
    label: "personal",
    credentialType: "oauth",
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    created: true,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function setup(isDesktop = false) {
  vi.resetModules();
  const openExternal = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({ isDesktop, desktop: { openExternal } }));
  const open = vi.spyOn(window, "open").mockReturnValue(null);
  const { useAccountLogin } = await import("@/hooks/use-account-login");
  const { renderHookWithDaemon } = await import("@/test/render-with-daemon");
  const { FakeUsageDaemon } = await import("@/test/usage-test-client");
  const { FakeWsClient } = await import("@/test/fake-ws-client");
  const { queryKeys } = await import("@/lib/query-keys");

  const client = new FakeWsClient();
  const daemon = new FakeUsageDaemon([], client);
  const onConnected = vi.fn();
  const view = renderHookWithDaemon(() => useAccountLogin({ onConnected }), daemon, {
    initialProps: null,
  });
  const login = () => view.result.current;
  const start = async () => {
    await act(async () => {
      login().start({ provider: "anthropic", label: "personal" });
    });
  };
  const authorize = (loginId = "l1", url = "https://claude.ai/oauth/authorize?state=abc") =>
    act(async () => {
      client.emit("account.oauthStart", 0, "authorizeUrl", { loginId, url });
    });
  return {
    login,
    start,
    authorize,
    client,
    openExternal,
    open,
    onConnected,
    queryClient: view.queryClient(),
    queryKeys,
    unmount: view.unmount,
  };
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("useAccountLogin", () => {
  it("login hook goes starting then waiting on authorizeUrl", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    expect(login().state).toEqual({ phase: "starting", method: "browser" });
    expect(client.nth("account.oauthStart").params).toEqual({ provider: "anthropic", label: "personal" });
    await authorize();
    expect(login().state).toEqual({
      phase: "waiting",
      loginId: "l1",
      url: "https://claude.ai/oauth/authorize?state=abc",
      pasteOnly: false,
    });
  });

  it("login hook shows device code state on deviceCode", async () => {
    const { login, start, client } = await setup();
    await start();
    await act(async () => {
      client.emit("account.oauthStart", 0, "deviceCode", {
        loginId: "l2",
        verificationUrl: "https://example.com/device",
        userCode: "ABCD-1234",
        expiresAt: "2026-01-01T00:15:00Z",
        intervalSeconds: 5,
      });
    });
    expect(login().state).toEqual({
      phase: "device",
      loginId: "l2",
      verificationUrl: "https://example.com/device",
      userCode: "ABCD-1234",
      expiresAt: "2026-01-01T00:15:00Z",
      intervalSeconds: 5,
    });
  });

  it("login hook marks pasteOnly from the event", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize("l1", "https://a");
    expect(login().state).toMatchObject({ phase: "waiting", pasteOnly: false });
    await act(async () => {
      client.emit("account.oauthStart", 0, "authorizeUrl", { loginId: "l1", url: "https://a", pasteOnly: true });
    });
    expect(login().state).toMatchObject({ phase: "waiting", pasteOnly: true });
  });

  it("stale generation events are dropped after cancel", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize();
    await act(async () => {
      login().cancel();
    });
    expect(login().state).toEqual({ phase: "idle" });
    await act(async () => {
      client.emit("account.oauthStart", 0, "deviceCode", {
        loginId: "l1",
        verificationUrl: "https://v",
        userCode: "X",
        expiresAt: "e",
        intervalSeconds: 5,
      });
      client.nth("account.oauthStart").resolve(accountResult());
    });
    await flush();
    expect(login().state).toEqual({ phase: "idle" });
  });

  it("cancel aborts the in-flight request and returns to idle", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize();
    const signal = client.nth("account.oauthStart").options?.signal;
    expect(signal?.aborted).toBe(false);
    await act(async () => {
      login().cancel();
    });
    expect(login().state).toEqual({ phase: "idle" });
    expect(login().pasteError).toBeNull();
    expect(signal?.aborted).toBe(true);
  });

  it("desktop opens the authorize url once and web does not", async () => {
    const desktop = await setup(true);
    await desktop.start();
    await desktop.authorize("l1", "https://a");
    await desktop.authorize("l1", "https://a");
    expect(desktop.openExternal).toHaveBeenCalledTimes(1);
    expect(desktop.openExternal).toHaveBeenCalledWith("https://a");
    await desktop.authorize("l2", "https://b");
    expect(desktop.openExternal).toHaveBeenCalledTimes(2);

    const web = await setup(false);
    await web.start();
    await web.authorize();
    expect(web.openExternal).not.toHaveBeenCalled();
    expect(web.open).not.toHaveBeenCalled();
  });

  it("paste complete sends oauthComplete with the loginId", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize("l9");
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    const call = client.nth("account.oauthComplete");
    expect(call.params).toEqual({ loginId: "l9", redirectUrl: PASTED_URL });
    expect(login().pasting).toBe(true);
    await act(async () => {
      call.resolve(null);
    });
    await flush();
    // An accepted paste keeps Finish disabled (pasteSubmitted is exposed as
    // pasting) until the login leaves waiting.
    expect(login().pasting).toBe(true);
    expect(login().state).toMatchObject({ phase: "waiting", loginId: "l9" });
    expect(login().pasteError).toBeNull();
  });

  it("paste errors keep the login waiting and show the message", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize();
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    await act(async () => {
      client.nth("account.oauthComplete").reject(new Error("account.oauthComplete: login_bad_redirect: nope"));
    });
    await flush();
    expect(login().state).toMatchObject({ phase: "waiting" });
    expect(login().pasteError).toBe("That doesn't look like the sign-in redirect address. Copy the whole address from the browser's address bar.");
    expect(login().pasting).toBe(false);
  });

  it("paste on an ended login moves to the error state", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize();
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    await act(async () => {
      client.nth("account.oauthComplete").reject(new Error("account.oauthComplete: login_unknown: gone"));
    });
    await flush();
    expect(login().state).toEqual({
      phase: "error",
      code: "login_unknown",
      message: "This sign-in is no longer active. Start again.",
      soft: false,
    });
  });

  it("login hook maps errors to a code, message and soft flag", async () => {
    const { login, start, client } = await setup();
    await start();
    await act(async () => {
      client.nth("account.oauthStart").reject(new Error("account.oauthStart: denied: user said no"));
    });
    await flush();
    expect(login().state).toEqual({
      phase: "error",
      code: "denied",
      message: "The sign-in was cancelled or denied.",
      soft: true,
    });
    await act(async () => {
      login().reset();
    });
    expect(login().state).toEqual({ phase: "idle" });
  });

  it("login result is upserted into the account cache and onConnected fires", async () => {
    const { login, start, client, queryClient, queryKeys, onConnected } = await setup();
    const existing = { ...accountResult(), id: 1, label: "work" };
    queryClient.setQueryData(queryKeys.accountList(), [existing]);
    await start();
    const result = accountResult();
    await act(async () => {
      client.nth("account.oauthStart").resolve(result);
    });
    await flush();
    expect(queryClient.getQueryData(queryKeys.accountList())).toEqual([existing, result]);
    expect(login().state).toEqual({ phase: "done", result });
    expect(onConnected).toHaveBeenCalledWith(result);
  });

  it("login hook prefers an explicit client", async () => {
    vi.resetModules();
    vi.doMock("@/lib/platform", () => ({ isDesktop: false, desktop: {} }));
    const { useAccountLogin } = await import("@/hooks/use-account-login");
    const { renderHookWithDaemon } = await import("@/test/render-with-daemon");
    const { FakeUsageDaemon } = await import("@/test/usage-test-client");
    const { FakeWsClient } = await import("@/test/fake-ws-client");

    const ambient = new FakeWsClient();
    const daemon = new FakeUsageDaemon([], ambient);
    const explicit = new FakeWsClient();
    const view = renderHookWithDaemon(() => useAccountLogin({ client: explicit }), daemon, {
      initialProps: null,
    });
    await act(async () => {
      view.result.current.start({ provider: "anthropic", label: "personal" });
    });
    expect(explicit.nth("account.oauthStart").params).toEqual({ provider: "anthropic", label: "personal" });
    expect(ambient.calls.filter((c) => c.method === "account.oauthStart")).toHaveLength(0);
  });

  it("a second paste cannot flip a successful login into an error", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize("l1");
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    await act(async () => {
      client.nth("account.oauthComplete").resolve(null);
    });
    await flush();

    // The accepted paste leaves the login waiting for its result, with the
    // paste already submitted: a second paste must be a no-op.
    await act(async () => {
      void login().complete("http://127.0.0.1:1/callback?code=OTHER");
      await Promise.resolve();
    });
    expect(client.calls.filter((c) => c.method === "account.oauthComplete")).toHaveLength(1);
    expect(login().state).toMatchObject({ phase: "waiting", loginId: "l1" });
    expect(login().pasteError).toBeNull();
  });

  it("a paste rejected after cancel does not touch state", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize("l1");
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    await act(async () => {
      login().cancel();
    });
    expect(login().state).toEqual({ phase: "idle" });
    await act(async () => {
      client.nth("account.oauthComplete").reject(new Error("account.oauthComplete: login_unknown: gone"));
    });
    await flush();
    expect(login().state).toEqual({ phase: "idle" });
    expect(login().pasteError).toBeNull();
    expect(login().pasting).toBe(false);
  });

  it("finish stays disabled after an accepted paste", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize("l1");
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    expect(login().pasting).toBe(true);
    await act(async () => {
      client.nth("account.oauthComplete").resolve(null);
    });
    await flush();
    // Still "pasting" (the panel's Finish stays disabled) until the login
    // leaves waiting.
    expect(login().pasting).toBe(true);
    expect(login().state).toMatchObject({ phase: "waiting" });
  });

  it("connect stays disabled until a cancelled login settles", async () => {
    const { login, start, client } = await setup();
    await start();
    await act(async () => {
      login().cancel();
    });
    expect(login().state).toEqual({ phase: "idle" });
    expect(login().cancelling).toBe(true);
    await act(async () => {
      client.nth("account.oauthStart").reject(new Error("account.oauthStart: cancelled"));
    });
    await flush();
    expect(login().cancelling).toBe(false);
  });

  it("a login that succeeds during cancel still updates the account cache", async () => {
    const { login, start, client, queryClient, queryKeys, onConnected } = await setup();
    await start();
    await act(async () => {
      login().cancel();
    });
    const result = accountResult();
    await act(async () => {
      client.nth("account.oauthStart").resolve(result);
    });
    await flush();
    // Idle stays visible, but the account exists now: the cache and
    // onConnected still reflect it.
    expect(login().state).toEqual({ phase: "idle" });
    expect(queryClient.getQueryData(queryKeys.accountList())).toEqual([result]);
    expect(onConnected).toHaveBeenCalledWith(result);
  });

  it("login hook never keeps the pasted url in state", async () => {
    const { login, start, client, authorize } = await setup();
    await start();
    await authorize();
    await act(async () => {
      void login().complete(PASTED_URL);
      await Promise.resolve();
    });
    await act(async () => {
      client.nth("account.oauthComplete").reject(new Error("account.oauthComplete: weird: nope"));
    });
    await flush();
    const whole = { ...login() };
    const serialized = JSON.stringify(whole);
    expect(serialized).not.toContain(PASTED_URL);
    expect(serialized).not.toContain("SECRET");
  });
});
