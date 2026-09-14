import { afterEach, describe, expect, it } from "vitest";

import {
  bindTerminal,
  boundTerminalId,
  hasTerminalActivity,
  markTerminalActivity,
  clearTerminalActivity,
  resetTerminalSessions,
  terminalIdsBoundElsewhere,
  unbindTerminal,
} from "@/lib/terminal-sessions";

afterEach(() => {
  resetTerminalSessions();
});

describe("terminal session bindings", () => {
  it("binds a tab to a session id, and forgets it on unbind", () => {
    bindTerminal("1:terminal", "term-1");
    expect(boundTerminalId("1:terminal")).toBe("term-1");

    unbindTerminal("1:terminal");
    expect(boundTerminalId("1:terminal")).toBeNull();
  });

  it("reports session ids owned by other tabs, excluding the asking tab's own", () => {
    bindTerminal("1:terminal", "term-1");
    bindTerminal("1:terminal:2", "term-2");

    expect(terminalIdsBoundElsewhere("1:terminal")).toEqual(new Set(["term-2"]));
    expect(terminalIdsBoundElsewhere("1:terminal:2")).toEqual(new Set(["term-1"]));
    expect(terminalIdsBoundElsewhere("1:terminal:3")).toEqual(new Set(["term-1", "term-2"]));
  });
});

describe("terminal activity flags", () => {
  it("marks and clears independently per tab", () => {
    markTerminalActivity("1:terminal:2");
    expect(hasTerminalActivity("1:terminal:2")).toBe(true);
    expect(hasTerminalActivity("1:terminal")).toBe(false);

    clearTerminalActivity("1:terminal:2");
    expect(hasTerminalActivity("1:terminal:2")).toBe(false);
  });
});
