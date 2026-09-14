import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ThemeProvider, useTheme } from "@/hooks/use-theme";
import { THEME_STORAGE_KEY } from "@/lib/theme";

/** A minimal controllable matchMedia stub -- lets a test flip "the OS prefers dark" and fire the 'change' listener ThemeProvider registers while pinned to "system". */
function stubMatchMedia(initialMatches: boolean = false) {
  let matches = initialMatches;
  const listeners = new Set<(e: MediaQueryListEvent) => void>();
  const mql = {
    get matches() {
      return matches;
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => listeners.add(cb),
    removeEventListener: (_: string, cb: (e: MediaQueryListEvent) => void) => listeners.delete(cb),
  } as unknown as MediaQueryList;
  window.matchMedia = vi.fn().mockReturnValue(mql);
  return {
    setMatches(next: boolean) {
      matches = next;
      for (const cb of listeners) cb({ matches: next } as MediaQueryListEvent);
    },
  };
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

describe("ThemeProvider/useTheme", () => {
  it("defaults to system; matchMedia reporting dark applies the dark class", () => {
    stubMatchMedia(true);
    renderHook(() => useTheme(), { wrapper: ThemeProvider });
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("flipping the media query while on system flips the class", () => {
    const media = stubMatchMedia(false);
    renderHook(() => useTheme(), { wrapper: ThemeProvider });
    expect(document.documentElement.classList.contains("dark")).toBe(false);

    act(() => media.setMatches(true));
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("pinned to light, the media query flipping does not change the class", () => {
    const media = stubMatchMedia(false);
    const { result } = renderHook(() => useTheme(), { wrapper: ThemeProvider });

    act(() => result.current.setPreference("light"));
    expect(document.documentElement.classList.contains("dark")).toBe(false);

    act(() => media.setMatches(true));
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("the preference round-trips through localStorage across a remount", () => {
    stubMatchMedia(false);
    const first = renderHook(() => useTheme(), { wrapper: ThemeProvider });
    act(() => first.result.current.setPreference("dark"));
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    first.unmount();

    const second = renderHook(() => useTheme(), { wrapper: ThemeProvider });
    expect(second.result.current.preference).toBe("dark");
    expect(second.result.current.resolved).toBe("dark");
  });
});
