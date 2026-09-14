import { afterEach, describe, expect, it } from "vitest";

import {
  applyResolvedTheme,
  computeBootstrapIsDark,
  readStoredThemePreference,
  resolveTheme,
  THEME_STORAGE_KEY,
  writeStoredThemePreference,
} from "@/lib/theme";

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.classList.remove("dark");
});

describe("resolveTheme", () => {
  it("resolves system to dark when the OS prefers dark", () => {
    expect(resolveTheme("system", true)).toBe("dark");
  });

  it("resolves system to light when the OS prefers light", () => {
    expect(resolveTheme("system", false)).toBe("light");
  });

  it("light/dark pass through regardless of the OS preference", () => {
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
  });
});

describe("readStoredThemePreference", () => {
  it("defaults to system with nothing persisted", () => {
    expect(readStoredThemePreference()).toBe("system");
  });

  it("round-trips a written preference", () => {
    writeStoredThemePreference("dark");
    expect(readStoredThemePreference()).toBe("dark");
  });

  it("falls back to system for a corrupt/unrecognized stored value", () => {
    window.localStorage.setItem(THEME_STORAGE_KEY, "sepia");
    expect(readStoredThemePreference()).toBe("system");
  });
});

describe("applyResolvedTheme", () => {
  it("adds the dark class for dark", () => {
    applyResolvedTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("removes the dark class for light", () => {
    document.documentElement.classList.add("dark");
    applyResolvedTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
});

describe("computeBootstrapIsDark", () => {
  it("a stored 'dark' preference wins regardless of the OS query", () => {
    expect(computeBootstrapIsDark("dark", false)).toBe(true);
  });

  it("a stored 'light' preference wins regardless of the OS query", () => {
    expect(computeBootstrapIsDark("light", true)).toBe(false);
  });

  it("no stored preference (or 'system') falls through to the OS query", () => {
    expect(computeBootstrapIsDark(null, true)).toBe(true);
    expect(computeBootstrapIsDark(null, false)).toBe(false);
    expect(computeBootstrapIsDark("system", true)).toBe(true);
  });
});
