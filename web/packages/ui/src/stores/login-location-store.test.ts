import { beforeEach, describe, expect, it } from "vitest";
import { useLoginLocationStore } from "@/stores/login-location-store";

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
});
