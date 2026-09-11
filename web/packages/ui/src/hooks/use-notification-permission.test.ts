import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useNotificationPermission } from "@/hooks/use-notification-permission";

/** Flushes pending microtasks, wrapped in `act` -- same helper other test files use. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** jsdom has no Notification API at all -- installs a minimal fake (permission + requestPermission, both mutable/spyable) so these tests can exercise the hook's actual logic instead of only its "unsupported" branch. */
function installFakeNotification(initialPermission: NotificationPermission) {
  const requestPermission = vi.fn<() => Promise<NotificationPermission>>();
  class FakeNotification {
    static permission: NotificationPermission = initialPermission;
    static requestPermission = requestPermission;
  }
  vi.stubGlobal("Notification", FakeNotification);
  return { FakeNotification, requestPermission };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useNotificationPermission", () => {
  it("reports 'unsupported' when the browser has no Notification API, and requestPermission is a safe no-op", () => {
    vi.unstubAllGlobals();
    const { result } = renderHook(() => useNotificationPermission());
    expect(result.current.permission).toBe("unsupported");

    expect(() => act(() => result.current.requestPermission())).not.toThrow();
    expect(result.current.permission).toBe("unsupported");
  });

  it("starts at whatever Notification.permission already is", () => {
    installFakeNotification("default");
    const { result } = renderHook(() => useNotificationPermission());
    expect(result.current.permission).toBe("default");
  });

  it("an explicit requestPermission call (from a default state) asks the browser once and adopts the result", async () => {
    const { requestPermission } = installFakeNotification("default");
    requestPermission.mockResolvedValue("granted");

    const { result } = renderHook(() => useNotificationPermission());
    act(() => {
      result.current.requestPermission();
    });
    await flush();

    expect(requestPermission).toHaveBeenCalledTimes(1);
    expect(result.current.permission).toBe("granted");
  });

  it("never re-prompts once permission is already decided -- a second requestPermission call after 'granted' doesn't call the native API again", async () => {
    const { requestPermission } = installFakeNotification("default");
    requestPermission.mockResolvedValue("granted");

    const { result } = renderHook(() => useNotificationPermission());
    act(() => {
      result.current.requestPermission();
    });
    await flush();
    expect(result.current.permission).toBe("granted");

    act(() => {
      result.current.requestPermission();
    });
    await flush();

    expect(requestPermission).toHaveBeenCalledTimes(1);
  });

  it("a browser that rejects requestPermission (e.g. called outside a user gesture) degrades to 'denied' without throwing, and never retries on its own", async () => {
    const { requestPermission } = installFakeNotification("default");
    requestPermission.mockRejectedValue(new Error("not allowed"));

    const { result } = renderHook(() => useNotificationPermission());
    expect(() =>
      act(() => {
        result.current.requestPermission();
      }),
    ).not.toThrow();
    await flush();

    expect(result.current.permission).toBe("denied");

    act(() => {
      result.current.requestPermission();
    });
    await flush();
    expect(requestPermission).toHaveBeenCalledTimes(1);
  });

  it("already-denied permission: requestPermission re-syncs state without calling the native API (no repeat prompt attempts)", () => {
    const { requestPermission } = installFakeNotification("denied");

    const { result } = renderHook(() => useNotificationPermission());
    expect(result.current.permission).toBe("denied");

    act(() => {
      result.current.requestPermission();
    });

    expect(requestPermission).not.toHaveBeenCalled();
    expect(result.current.permission).toBe("denied");
  });
});
