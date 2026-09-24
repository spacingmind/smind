import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { useNotificationSoundPreference } from "@/hooks/use-notification-sound-preference";
import { readNotificationSoundEnabled } from "@/lib/sidebar-preferences";

afterEach(() => {
  window.localStorage.clear();
});

describe("useNotificationSoundPreference", () => {
  it("defaults to off", () => {
    const { result } = renderHook(() => useNotificationSoundPreference());
    expect(result.current.enabled).toBe(false);
  });

  it("setEnabled persists and updates every mounted subscriber", () => {
    const a = renderHook(() => useNotificationSoundPreference());
    const b = renderHook(() => useNotificationSoundPreference());

    act(() => {
      a.result.current.setEnabled(true);
    });

    expect(a.result.current.enabled).toBe(true);
    expect(b.result.current.enabled).toBe(true);
    expect(readNotificationSoundEnabled()).toBe(true);
  });
});
