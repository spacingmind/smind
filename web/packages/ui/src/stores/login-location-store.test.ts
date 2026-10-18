import { beforeEach, describe, expect, it } from "vitest";
import { useLoginLocationOverride, useLoginLocationStore } from "@/stores/login-location-store";

describe("login-location-store", () => {
  beforeEach(() => {
    localStorage.clear();
    useLoginLocationStore.setState({ overrides: {} });
  });

  it("override is stored per connection id and survives a reload", () => {
    useLoginLocationStore.getState().setOverride("conn-a", "other-device");
    useLoginLocationStore.getState().setOverride("conn-b", "this-device");

    expect(useLoginLocationOverride("conn-a")).toBe("other-device");
    expect(useLoginLocationOverride("conn-b")).toBe("this-device");
    expect(useLoginLocationOverride("conn-c")).toBeNull();

    // Simulated reload: fresh state rehydrated from localStorage.
    useLoginLocationStore.setState({ overrides: {} });
    void useLoginLocationStore.persist.rehydrate();
    expect(useLoginLocationOverride("conn-a")).toBe("other-device");
    expect(useLoginLocationOverride("conn-b")).toBe("this-device");

    const stored = JSON.parse(localStorage.getItem("smind:login-location") ?? "{}");
    expect(stored.state.overrides).toEqual({ "conn-a": "other-device", "conn-b": "this-device" });

    // Clearing with null removes just that id.
    useLoginLocationStore.getState().setOverride("conn-a", null);
    expect(useLoginLocationOverride("conn-a")).toBeNull();
    expect(useLoginLocationOverride("conn-b")).toBe("this-device");
  });
});
