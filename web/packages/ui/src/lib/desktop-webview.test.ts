import { afterEach, describe, expect, it, vi } from "vitest";

// The install() entry points gate on lib/platform.ts's build-time
// isDesktop, so -- exactly like platform.test.ts -- each scenario stubs
// the env var and re-imports the module fresh rather than mutating an
// already-loaded instance.

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
  document.documentElement.removeAttribute("data-smind-desktop");
  document.documentElement.removeAttribute("data-smind-desktop-os");
});

describe("desktop-webview (D2 desktop gating)", () => {
  it("desktop-gating: markers and zoom guard install only when isDesktop", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const web = await import("@/lib/desktop-webview");
    web.installDesktopWebviewDefaults();
    expect(document.documentElement.hasAttribute("data-smind-desktop")).toBe(false);
    expect(document.documentElement.hasAttribute("data-smind-desktop-os")).toBe(false);

    const root = document.createElement("div");
    const wheel = vi.fn();
    root.addEventListener("wheel", wheel);
    root.dispatchEvent(
      new WheelEvent("wheel", { ctrlKey: true, cancelable: true }),
    );
    expect(wheel.mock.calls[0]?.[0]).toHaveProperty("defaultPrevented", false);

    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    vi.resetModules();
    const desktopWeb = await import("@/lib/desktop-webview");
    desktopWeb.installDesktopWebviewDefaults();
    expect(document.documentElement.getAttribute("data-smind-desktop")).toBe("");
    expect(["macos", "windows", "linux", "unknown"]).toContain(
      document.documentElement.getAttribute("data-smind-desktop-os"),
    );
  });

  it("zoom-guard: ctrl/cmd+wheel and gesture events are prevented; plain wheel and keys pass through", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { installDesktopZoomGuard } = await import("@/lib/desktop-webview");

    const root = document.createElement("div");
    installDesktopZoomGuard(root);

    function wheelOf(init: WheelEventInit): WheelEvent {
      const event = new WheelEvent("wheel", { cancelable: true, ...init });
      root.dispatchEvent(event);
      return event;
    }

    expect(wheelOf({ ctrlKey: true }).defaultPrevented).toBe(true);
    expect(wheelOf({ metaKey: true }).defaultPrevented).toBe(true);
    expect(wheelOf({}).defaultPrevented).toBe(false);
    expect(wheelOf({ shiftKey: true }).defaultPrevented).toBe(false);

    // jsdom has no GestureEvent constructor; a plain Event stands in --
    // the guard only calls preventDefault, which Event supports.
    const gesture = new Event("gesturestart", { cancelable: true });
    root.dispatchEvent(gesture);
    expect(gesture.defaultPrevented).toBe(true);
  });

  it("detectDesktopOs classifies windows, macos and linux user agents", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { detectDesktopOs } = await import("@/lib/desktop-webview");
    expect(detectDesktopOs("Mozilla/5.0 (Windows NT 10.0; Win64; x64)")).toBe("windows");
    expect(detectDesktopOs("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)")).toBe("macos");
    expect(detectDesktopOs("Mozilla/5.0 (X11; Linux x86_64)")).toBe("linux");
    expect(detectDesktopOs("smind-tests")).toBe("unknown");
  });
});
