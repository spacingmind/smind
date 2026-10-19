import { act } from "react";
import { cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// `isDesktop` is a module-load constant, so the platform is mocked per
// scenario and the hook imported fresh (use-account-login.test.tsx's
// pattern). The hostname goes through the exported `useHostname`
// indirection so each scenario can pin it without touching jsdom's
// read-only location.

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
  vi.resetModules();
});

async function setup(opts: {
  isDesktop: boolean;
  hostname: string;
  connection?: { id: string; kind: "local" | "url" | "relay" | "ssh" } | null;
}) {
  vi.resetModules();
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.isDesktop,
    desktop: { getCurrentConnection: vi.fn(() => Promise.resolve(opts.connection ?? null)) },
  }));
  const { useLoginRemote, hostnameSource } = await import("@/hooks/use-login-remote");
  const { useLoginLocationStore } = await import("@/stores/login-location-store");
  const { renderHookWithDaemon } = await import("@/test/render-with-daemon");
  const { FakeUsageDaemon } = await import("@/test/usage-test-client");
  const { FakeWsClient } = await import("@/test/fake-ws-client");

  const store = useLoginLocationStore;
  store.getState().setOverride("c1", null);
  hostnameSource.get = () => opts.hostname;

  const daemon = new FakeUsageDaemon([], new FakeWsClient());
  const view = renderHookWithDaemon(() => useLoginRemote(), daemon, { initialProps: null });
  const remote = () => view.result.current;
  return { remote, store, unmount: view.unmount };
}

const flush = () => act(async () => { await Promise.resolve(); });

describe("useLoginRemote", () => {
  it("login remote hook derives remote from the desktop connection kind", async () => {
    const url = await setup({ isDesktop: true, hostname: "127.0.0.1", connection: { id: "c1", kind: "url" } });
    await flush();
    expect(url.remote().remote).toBe(true);
    expect(url.remote().derivedRemote).toBe(true);

    const local = await setup({ isDesktop: true, hostname: "127.0.0.1", connection: { id: "c1", kind: "local" } });
    await flush();
    expect(local.remote().remote).toBe(false);
    expect(local.remote().derivedRemote).toBe(false);
  });

  it("login remote hook uses the hostname on web", async () => {
    const remote = await setup({ isDesktop: false, hostname: "my-daemon.example.com" });
    await flush();
    expect(remote.remote().remote).toBe(true);

    const local = await setup({ isDesktop: false, hostname: "localhost" });
    await flush();
    expect(local.remote().remote).toBe(false);
  });

  it("login remote hook override wins and persists per connection", async () => {
    const s = await setup({ isDesktop: true, hostname: "127.0.0.1", connection: { id: "c1", kind: "url" } });
    await flush();
    expect(s.remote().remote).toBe(true);

    await act(async () => {
      s.remote().setOverride("this-device");
    });
    await flush();
    expect(s.remote().remote).toBe(false);
    expect(s.remote().override).toBe("this-device");

    // Persists in the store (a fresh render of the same connection sees it)
    // and is scoped to the connection id.
    expect(s.store.getState().overrides).toEqual({ c1: "this-device" });
    expect(s.store.getState().overrides["web"]).toBeUndefined();

    await act(async () => {
      s.remote().setOverride(null);
    });
    await flush();
    expect(s.remote().remote).toBe(true);
  });
});
