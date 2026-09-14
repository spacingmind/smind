import { afterEach, describe, expect, it } from "vitest";

import {
  DEFAULT_TERMINAL_SCROLLBACK,
  TERMINAL_SCROLLBACK_STORAGE_KEY,
  loadTerminalScrollback,
  saveTerminalScrollback,
} from "@/lib/terminal-prefs";

afterEach(() => {
  window.localStorage.clear();
});

describe("terminal scrollback prefs", () => {
  it("defaults when nothing is stored", () => {
    expect(loadTerminalScrollback()).toBe(DEFAULT_TERMINAL_SCROLLBACK);
  });

  it("round-trips a saved value", () => {
    saveTerminalScrollback(5000);
    expect(loadTerminalScrollback()).toBe(5000);
    expect(window.localStorage.getItem(TERMINAL_SCROLLBACK_STORAGE_KEY)).toBe("5000");
  });

  it("clamps a corrupted or hand-edited value to a sane range", () => {
    window.localStorage.setItem(TERMINAL_SCROLLBACK_STORAGE_KEY, "-50");
    expect(loadTerminalScrollback()).toBeGreaterThanOrEqual(100);

    window.localStorage.setItem(TERMINAL_SCROLLBACK_STORAGE_KEY, "not-a-number");
    expect(loadTerminalScrollback()).toBe(DEFAULT_TERMINAL_SCROLLBACK);

    window.localStorage.setItem(TERMINAL_SCROLLBACK_STORAGE_KEY, "999999999");
    expect(loadTerminalScrollback()).toBeLessThanOrEqual(100_000);
  });
});
