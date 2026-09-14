import { describe, expect, it } from "vitest";

import {
  comboStringFromEvent,
  DIGIT_WILDCARD,
  formatCombo,
  matchCombo,
  parseCombo,
  SHORTCUT_KEY_NAMES,
  type KeyEventLike,
} from "@/keyboard/shortcut-string";

/** A KeyboardEvent-shaped plain object; every modifier defaults off. */
function event(over: Partial<KeyEventLike> & { key: string }): KeyEventLike {
  return { altKey: false, ctrlKey: false, metaKey: false, shiftKey: false, ...over };
}

describe("parseCombo", () => {
  it("parses modifiers and the key part", () => {
    expect(parseCombo("Mod+Alt+T")).toEqual({ code: "KeyT", key: "t", mod: true, alt: true });
    expect(parseCombo("Shift+?")).toEqual({ code: "Slash", key: "?", shift: true });
    expect(parseCombo("Escape")).toEqual({ code: "Escape", key: "escape" });
  });

  it("parses the Digit wildcard", () => {
    expect(parseCombo("Mod+Alt+Digit")).toEqual({ code: DIGIT_WILDCARD, mod: true, alt: true });
  });

  it("throws on a malformed or unknown combo rather than silently never firing", () => {
    expect(() => parseCombo("Mod+")).toThrow(/invalid shortcut string/);
    expect(() => parseCombo("Mod")).toThrow(/no key part/);
    expect(() => parseCombo("Mod+K+L")).toThrow(/two key parts/);
    expect(() => parseCombo("Mod+F13")).toThrow(/unknown key/);
  });

  it("exports every spellable key name for a rebinding UI to validate against", () => {
    expect(SHORTCUT_KEY_NAMES).toContain("K");
    expect(SHORTCUT_KEY_NAMES).toContain("Escape");
    expect(SHORTCUT_KEY_NAMES).toContain(DIGIT_WILDCARD);
  });
});

describe("matchCombo", () => {
  it("resolves Mod to Cmd on mac and Ctrl elsewhere", () => {
    const combo = parseCombo("Mod+K");
    expect(matchCombo(combo, event({ key: "k", code: "KeyK", metaKey: true }), true)).toEqual({});
    expect(matchCombo(combo, event({ key: "k", code: "KeyK", ctrlKey: true }), true)).toBeNull();
    expect(matchCombo(combo, event({ key: "k", code: "KeyK", ctrlKey: true }), false)).toEqual({});
    expect(matchCombo(combo, event({ key: "k", code: "KeyK", metaKey: true }), false)).toBeNull();
  });

  it("matches modifiers exactly, so a near-miss does not fire", () => {
    const combo = parseCombo("Mod+K");
    expect(
      matchCombo(combo, event({ key: "k", code: "KeyK", ctrlKey: true, shiftKey: true }), false),
    ).toBeNull();
    expect(
      matchCombo(combo, event({ key: "k", code: "KeyK", ctrlKey: true, altKey: true }), false),
    ).toBeNull();
    expect(matchCombo(combo, event({ key: "k", code: "KeyK" }), false)).toBeNull();
  });

  it("prefers code over key, so a non-US layout still matches", () => {
    // Alt+T on mac produces key "†" -- code is what survives.
    const combo = parseCombo("Mod+Alt+T");
    expect(
      matchCombo(combo, event({ key: "†", code: "KeyT", metaKey: true, altKey: true }), true),
    ).toEqual({});
  });

  it("falls back to key when the event carries no code", () => {
    const combo = parseCombo("Mod+K");
    expect(matchCombo(combo, event({ key: "K", ctrlKey: true }), false)).toEqual({});
  });

  it("yields the pressed digit for the Digit wildcard", () => {
    const combo = parseCombo("Mod+Alt+Digit");
    expect(
      matchCombo(combo, event({ key: "3", code: "Digit3", ctrlKey: true, altKey: true }), false),
    ).toEqual({ digit: 3 });
    expect(
      matchCombo(combo, event({ key: "k", code: "KeyK", ctrlKey: true, altKey: true }), false),
    ).toBeNull();
  });
});

describe("formatCombo", () => {
  it("renders mac glyphs without separators and Ctrl-style elsewhere", () => {
    expect(formatCombo("Mod+Alt+T", true)).toBe("⌘⌥T");
    expect(formatCombo("Mod+Alt+T", false)).toBe("Ctrl+Alt+T");
  });

  it("drops the redundant Shift from Shift+? on both platforms", () => {
    // The `?` glyph already implies Shift on every layout that has it, so
    // rendering "⇧?" / "Shift+?" would spell the same modifier twice.
    expect(formatCombo("Shift+?", true)).toBe("?");
    expect(formatCombo("Shift+?", false)).toBe("?");
  });

  it("labels special keys and the digit wildcard", () => {
    expect(formatCombo("Escape", false)).toBe("Esc");
    expect(formatCombo("Mod+Alt+Digit", false)).toBe("Ctrl+Alt+1–9");
  });
});

describe("comboStringFromEvent", () => {
  it("spells the combo a captured event represents", () => {
    expect(comboStringFromEvent(event({ key: "k", code: "KeyK", metaKey: true }))).toBe("Cmd+K");
    expect(
      comboStringFromEvent(event({ key: "t", code: "KeyT", ctrlKey: true, altKey: true })),
    ).toBe("Ctrl+Alt+T");
    expect(comboStringFromEvent(event({ key: "Escape", code: "Escape" }))).toBe("Escape");
  });

  it("returns null for a key it has no name for", () => {
    expect(comboStringFromEvent(event({ key: "F13" }))).toBeNull();
  });
});
