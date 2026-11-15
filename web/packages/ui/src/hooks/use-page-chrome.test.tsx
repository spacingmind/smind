// The left inset a full-page view's header needs. `vi.doMock` of @/lib/platform, so this lives in its own file.
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
  vi.doUnmock("@/lib/platform");
});

async function load(os: "macos" | "windows" | null, desktop: boolean) {
  vi.doMock("@/lib/platform", () => ({
    isDesktop: desktop,
    desktopOS: os,
    desktopWindow: { onStateChange: (cb: (s: { maximized: boolean; fullscreen: boolean }) => void) => (cb({ maximized: false, fullscreen: false }), () => {}) },
  }));
  return import("@/hooks/use-page-chrome");
}

describe("usePageLeftInset", () => {
  it("clears the traffic lights on macOS desktop when the page covers the window, and not when the sidebar is kept", async () => {
    const { PageChromeContext, usePageLeftInset } = await load("macos", true);
    const wrap = (kept: boolean) => ({ children }: { children: ReactNode }) => <PageChromeContext.Provider value={{ sidebarKept: kept }}>{children}</PageChromeContext.Provider>;
    expect(renderHook(() => usePageLeftInset(), { wrapper: wrap(false) }).result.current).toBeGreaterThan(0);
    expect(renderHook(() => usePageLeftInset(), { wrapper: wrap(true) }).result.current).toBe(0);
  });

  it("is zero in a browser build and on Windows, kept or not", async () => {
    const browser = await load(null, false);
    expect(renderHook(() => browser.usePageLeftInset()).result.current).toBe(0);
    vi.resetModules();
    const win = await load("windows", true);
    expect(renderHook(() => win.usePageLeftInset()).result.current).toBe(0);
  });
});
