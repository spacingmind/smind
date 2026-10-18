import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useLoginLocationStore, LOGIN_LOCATION_STORAGE_KEY } from "@/stores/login-location-store";

describe("login-location-store", () => {
  beforeEach(() => {
    localStorage.clear();
    useLoginLocationStore.setState({ overrides: {} });
  });

  it("override is stored per connection id and survives a reload", async () => {
    useLoginLocationStore.getState().setOverride("conn-a", "other-device");
    useLoginLocationStore.getState().setOverride("conn-b", "this-device");

    expect(useLoginLocationStore.getState().overrides["conn-a"]).toBe("other-device");
    expect(useLoginLocationStore.getState().overrides["conn-b"]).toBe("this-device");
    expect(useLoginLocationStore.getState().overrides["conn-c"]).toBeUndefined();

    // Simulated reload: reset the live state (restoring the persisted
    // JSON afterwards -- persist writes on setState, which would otherwise
    // blank localStorage), then rehydrate from storage.
    const saved = localStorage.getItem("smind:login-location");
    useLoginLocationStore.setState({ overrides: {} });
    localStorage.setItem("smind:login-location", saved ?? "{}");
    await useLoginLocationStore.persist.rehydrate();
    expect(useLoginLocationStore.getState().overrides["conn-a"]).toBe("other-device");
    expect(useLoginLocationStore.getState().overrides["conn-b"]).toBe("this-device");

    const stored = JSON.parse(localStorage.getItem("smind:login-location") ?? "{}");
    expect(stored.state.overrides).toEqual({ "conn-a": "other-device", "conn-b": "this-device" });

    // Clearing with null removes just that id.
    useLoginLocationStore.getState().setOverride("conn-a", null);
    expect(useLoginLocationStore.getState().overrides["conn-a"]).toBeUndefined();
    expect(useLoginLocationStore.getState().overrides["conn-b"]).toBe("this-device");
  });

  it("store drops a corrupt persisted shape", async () => {
    localStorage.setItem(
      LOGIN_LOCATION_STORAGE_KEY,
      JSON.stringify({ state: { overrides: { c1: "somewhere-else", o: {} } }, version: 9 }),
    );
    vi.resetModules();
    const fresh = await import("@/stores/login-location-store");
    expect(fresh.useLoginLocationStore.getState().overrides).toEqual({});
  });

  it("setOverride before the connection resolves is ignored", async () => {
    vi.resetModules();
    vi.doMock("@/lib/platform", () => ({
      isDesktop: true,
      desktop: { getCurrentConnection: () => new Promise(() => {}) },
    }));
    const { useLoginRemote } = await import("@/hooks/use-login-remote");
    const { renderHookWithDaemon } = await import("@/test/render-with-daemon");
    const { FakeUsageDaemon } = await import("@/test/usage-test-client");
    const { FakeWsClient } = await import("@/test/fake-ws-client");
    const daemon = new FakeUsageDaemon([], new FakeWsClient());
    const view = renderHookWithDaemon(() => useLoginRemote(), daemon, { initialProps: null });
    const fresh = await import("@/stores/login-location-store");
    const before = JSON.stringify(fresh.useLoginLocationStore.getState().overrides);
    await act(async () => {
      view.result.current.setOverride("other-device");
    });
    expect(JSON.stringify(fresh.useLoginLocationStore.getState().overrides)).toBe(before);
    expect(before).toBe("{}");
  });
});
