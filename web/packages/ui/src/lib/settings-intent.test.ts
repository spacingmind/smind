import { afterEach, describe, expect, it, vi } from "vitest";

import { setSettingsIntent, subscribeSettingsIntent, takeSettingsIntent } from "@/lib/settings-intent";

afterEach(() => {
  takeSettingsIntent();
});

describe("settings-intent", () => {
  it("take returns the intent once, then null", () => {
    setSettingsIntent({ kind: "editProfile", id: 3 });
    expect(takeSettingsIntent()).toEqual({ kind: "editProfile", id: 3 });
    expect(takeSettingsIntent()).toBeNull();
  });

  it("a later set replaces an untaken one", () => {
    setSettingsIntent({ kind: "editProfile", id: 1 });
    setSettingsIntent({ kind: "newProfile", seed: { name: "n", provider: "glm", model: "m" } });
    expect(takeSettingsIntent()?.kind).toBe("newProfile");
  });

  it("notifies subscribers on every set until unsubscribed", () => {
    const listener = vi.fn();
    const off = subscribeSettingsIntent(listener);
    setSettingsIntent({ kind: "editProfile", id: 1 });
    setSettingsIntent({ kind: "editProfile", id: 2 });
    expect(listener).toHaveBeenCalledTimes(2);
    off();
    setSettingsIntent({ kind: "editProfile", id: 3 });
    expect(listener).toHaveBeenCalledTimes(2);
  });
});
