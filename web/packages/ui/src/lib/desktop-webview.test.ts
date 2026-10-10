import { afterEach, describe, expect, it, vi } from "vitest";

// The install() entry point gates on lib/platform.ts's build-time
// isDesktop, so -- exactly like platform.test.ts -- each scenario stubs
// the env var and re-imports the module fresh rather than mutating an
// already-loaded instance.

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("desktop-webview (D2.5, D2.8)", () => {
  it("desktop-gating: installDesktopWebviewDefaults is inert in the browser build", async () => {
    // Runs first in this file, before any desktop scenario has had a
    // chance to add a document-level listener.
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const web = await import("@/lib/desktop-webview");
    web.installDesktopWebviewDefaults();
    const wheel = new WheelEvent("wheel", { ctrlKey: true, cancelable: true });
    document.documentElement.dispatchEvent(wheel);
    expect(wheel.defaultPrevented).toBe(false);
  });

  it("desktop-gating: installDesktopWebviewDefaults installs the guard in the desktop build", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const web = await import("@/lib/desktop-webview");
    web.installDesktopWebviewDefaults();
    const wheel = new WheelEvent("wheel", { ctrlKey: true, cancelable: true });
    document.documentElement.dispatchEvent(wheel);
    expect(wheel.defaultPrevented).toBe(true);
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
});
