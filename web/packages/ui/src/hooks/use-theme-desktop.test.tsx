// desktop-native-feel W4: on desktop, "system" follows the native window
// theme (WebView2's matchMedia can disagree with the OS). `@/lib/platform` is
// a module-load constant, so it is mocked per scenario and the provider
// imported fresh.
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

function stubMatchMedia(matches: boolean) {
  window.matchMedia = vi.fn().mockReturnValue({
    matches,
    media: "(prefers-color-scheme: dark)",
    addEventListener: () => {},
    removeEventListener: () => {},
  } as unknown as MediaQueryList);
}

function mockPlatform(opts: { desktop: boolean; native?: "light" | "dark" }) {
  let emit: ((t: "light" | "dark") => void) | null = null;
  const setTheme = vi.fn(() => Promise.resolve());
  vi.doMock("@/lib/platform", () => ({
    isDesktop: opts.desktop,
    desktopOS: opts.desktop ? "windows" : null,
    desktopWindow: { setTheme },
    desktop: {
      getSystemTheme: () => Promise.resolve(opts.native ?? "light"),
      onSystemThemeChanged: (cb: (t: "light" | "dark") => void) => {
        emit = cb;
        return () => {
          emit = null;
        };
      },
    },
  }));
  return { setTheme, emit: (t: "light" | "dark") => emit?.(t) };
}

const isDark = () => document.documentElement.classList.contains("dark");

describe("ThemeProvider system theme", () => {
  it("desktop system theme follows the native window theme, not matchMedia", async () => {
    stubMatchMedia(false); // WebView2 reports light...
    const platform = mockPlatform({ desktop: true, native: "dark" }); // ...the OS is dark
    const { ThemeProvider, useTheme } = await import("@/hooks/use-theme");
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider });
    await waitFor(() => expect(result.current.resolved).toBe("dark"));
    expect(isDark()).toBe(true);
    // The native window is told "system" (not forced) and the corrected surface.
    await waitFor(() => expect(platform.setTheme).toHaveBeenLastCalledWith("system", "dark"));
  });

  it("desktop native theme change re-resolves system", async () => {
    stubMatchMedia(false);
    const platform = mockPlatform({ desktop: true, native: "light" });
    const { ThemeProvider, useTheme } = await import("@/hooks/use-theme");
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider });
    await waitFor(() => expect(platform.setTheme).toHaveBeenCalled());
    expect(result.current.resolved).toBe("light");

    act(() => platform.emit("dark"));
    expect(result.current.resolved).toBe("dark");
    expect(isDark()).toBe(true);
    act(() => platform.emit("light"));
    expect(isDark()).toBe(false);
  });

  it("web system theme still follows matchMedia", async () => {
    stubMatchMedia(true);
    mockPlatform({ desktop: false, native: "light" });
    const { ThemeProvider, useTheme } = await import("@/hooks/use-theme");
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider });
    expect(result.current.resolved).toBe("dark");
    expect(isDark()).toBe(true);
  });
});
