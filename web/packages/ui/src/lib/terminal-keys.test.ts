import { describe, expect, it } from "vitest";

import { encodeTerminalKey, resolvePendingModifierData, terminalKeyInput } from "@/lib/terminal-keys";

const NONE = { ctrl: false, shift: false, alt: false };
const enc = (key: string, mods: Partial<typeof NONE> = {}, appCursor = false) =>
  encodeTerminalKey(terminalKeyInput(key, { ...NONE, ...mods }), { applicationCursorKeys: appCursor });

describe("encodeTerminalKey", () => {
  it("Escape sends ESC", () => expect(enc("Escape")).toBe("\x1b"));
  it("Tab sends HT", () => expect(enc("Tab")).toBe("\t"));
  it("Shift+Tab sends CSI Z", () => expect(enc("Tab", { shift: true })).toBe("\x1b[Z"));
  it("Enter sends CR", () => expect(enc("Enter")).toBe("\r"));
  it("Backspace sends DEL", () => expect(enc("Backspace")).toBe("\x7f"));
  it("ArrowUp/Down/Right/Left send CSI A/B/C/D", () => {
    expect(enc("ArrowUp")).toBe("\x1b[A");
    expect(enc("ArrowDown")).toBe("\x1b[B");
    expect(enc("ArrowRight")).toBe("\x1b[C");
    expect(enc("ArrowLeft")).toBe("\x1b[D");
  });
  it("arrows send SS3 in application cursor mode", () => {
    expect(enc("ArrowUp", {}, true)).toBe("\x1bOA");
    expect(enc("ArrowLeft", {}, true)).toBe("\x1bOD");
  });
  it("Ctrl+Up sends CSI 1;5 A", () => expect(enc("ArrowUp", { ctrl: true })).toBe("\x1b[1;5A"));
  it("Ctrl+c sends ETX", () => expect(enc("c", { ctrl: true })).toBe("\x03"));
  it("Ctrl+[ sends ESC", () => expect(enc("[", { ctrl: true })).toBe("\x1b"));
  it("Alt+x sends ESC x", () => expect(enc("x", { alt: true })).toBe("\x1bx"));
  it("Shift+a sends A", () => expect(enc("a", { shift: true })).toBe("A"));
});

describe("resolvePendingModifierData", () => {
  it("data mapping: a pending modifier consumes the next printable key", () => {
    const pending = { ...NONE, ctrl: true };
    expect(resolvePendingModifierData({ data: "c", pending })).toEqual({ mode: "key", key: "c", clearPendingModifiers: true });
    expect(resolvePendingModifierData({ data: "\r", pending })).toMatchObject({ mode: "key", key: "Enter" });
    expect(resolvePendingModifierData({ data: "\x7f", pending })).toMatchObject({ mode: "key", key: "Backspace" });
  });
  it("data mapping: non-printable data stays raw but still clears", () => {
    expect(resolvePendingModifierData({ data: "\x1b[A", pending: { ...NONE, alt: true } })).toEqual({
      mode: "raw",
      clearPendingModifiers: true,
    });
    expect(resolvePendingModifierData({ data: "c", pending: NONE })).toEqual({ mode: "raw", clearPendingModifiers: false });
  });
});
