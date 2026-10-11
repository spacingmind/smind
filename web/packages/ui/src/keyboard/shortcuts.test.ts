import { describe, expect, it } from "vitest";

import {
  bindingAllowedInScope,
  platformBindings,
  conflictingBindings,
  helpSections,
  INITIAL_CHORD_STATE,
  matchShortcut,
  resolveBindings,
  resolveChordStep,
  SECTION_ORDER,
  SHORTCUT_BINDINGS,
  UNASSIGNED,
  type ChordState,
  type ShortcutBinding,
} from "@/keyboard/shortcuts";
import { canonicalChord, type KeyEventLike } from "@/keyboard/shortcut-string";

function event(over: Partial<KeyEventLike> & { key: string }): KeyEventLike {
  return { altKey: false, ctrlKey: false, metaKey: false, shiftKey: false, ...over };
}

const NON_MAC = { isMac: false, scope: "other" } as const;

describe("SHORTCUT_BINDINGS", () => {
  it("parses every shipped combo", () => {
    // resolveBindings throws only for a *default* combo it can't parse --
    // overrides are tolerated. So this is the load-bearing assertion that
    // no binding was added with a typo'd key name.
    expect(() => resolveBindings()).not.toThrow();
  });

  it("has unique ids and no two bindings on the same combo", () => {
    const ids = SHORTCUT_BINDINGS.map((b) => b.id);
    expect(new Set(ids).size).toBe(ids.length);
    const combos = SHORTCUT_BINDINGS.map((b) => b.combo);
    expect(new Set(combos).size).toBe(combos.length);
  });

  it("puts every binding in a known section", () => {
    for (const binding of SHORTCUT_BINDINGS) {
      expect(SECTION_ORDER).toContain(binding.section);
    }
  });

  it("binds the combos the plan names, matching Paseo's defaults", () => {
    // The web table: jsdom is not a desktop build, and the desktop-only
    // Mod+T / Mod+Digit rows would shadow the web defaults in the map.
    const byAction = new Map(
      platformBindings(SHORTCUT_BINDINGS, false, false)
        .filter((b) => !b.id.endsWith("-terminal"))
        .map((b) => [b.action, b.combo]),
    );
    expect(byAction.get("sidebar.toggle")).toBe("Mod+B");
    expect(byAction.get("palette.open")).toBe("Mod+K");
    expect(byAction.get("composer.focus")).toBe("Mod+L");
    expect(byAction.get("tab.close")).toBe("Mod+W");
    expect(byAction.get("tab.jump")).toBe("Mod+Alt+Digit");
    expect(byAction.get("task.prev")).toBe("Mod+[");
    expect(byAction.get("task.next")).toBe("Mod+]");
    expect(byAction.get("run.interrupt")).toBe("Escape");
    expect(byAction.get("theme.cycle")).toBe("Mod+Alt+T");
    expect(byAction.get("shortcuts.help")).toBe("Shift+?");
  });
});

describe("matchShortcut", () => {
  const bindings = resolveBindings();

  it("fires the right binding for the right event", () => {
    const match = matchShortcut(bindings, event({ key: "k", code: "KeyK", ctrlKey: true }), NON_MAC);
    expect(match?.action).toBe("palette.open");
    expect(match?.payload).toBeNull();
  });

  it("does not fire for a near-miss modifier", () => {
    expect(
      matchShortcut(
        bindings,
        event({ key: "k", code: "KeyK", ctrlKey: true, shiftKey: true }),
        NON_MAC,
      ),
    ).toBeNull();
  });

  it("does not fire the wrong platform variant", () => {
    // Cmd+K on a non-mac is nobody's binding: Mod resolves to Ctrl there.
    expect(
      matchShortcut(bindings, event({ key: "k", code: "KeyK", metaKey: true }), NON_MAC),
    ).toBeNull();
    expect(
      matchShortcut(bindings, event({ key: "k", code: "KeyK", metaKey: true }), {
        isMac: true,
        scope: "other",
      })?.action,
    ).toBe("palette.open");
  });

  it("hands the pressed digit to a Digit-wildcard binding", () => {
    const match = matchShortcut(
      bindings,
      event({ key: "2", code: "Digit2", ctrlKey: true, altKey: true }),
      NON_MAC,
    );
    expect(match?.action).toBe("tab.jump");
    expect(match?.payload).toEqual({ digit: 2 });
  });

  it("ignores auto-repeat", () => {
    expect(
      matchShortcut(
        bindings,
        event({ key: "k", code: "KeyK", ctrlKey: true, repeat: true }),
        NON_MAC,
      ),
    ).toBeNull();
  });

  it("blocks a non-global binding in an editable or terminal scope but not elsewhere", () => {
    const help = event({ key: "?", code: "Slash", shiftKey: true });
    expect(matchShortcut(bindings, help, NON_MAC)?.action).toBe("shortcuts.help");
    expect(matchShortcut(bindings, help, { isMac: false, scope: "editable" })).toBeNull();
    expect(matchShortcut(bindings, help, { isMac: false, scope: "terminal" })).toBeNull();
  });

  it("lets a global binding through an editable scope", () => {
    const palette = event({ key: "k", code: "KeyK", ctrlKey: true });
    expect(matchShortcut(bindings, palette, { isMac: false, scope: "editable" })?.action).toBe(
      "palette.open",
    );
  });

  it("blocks every binding, global included, while a modal owns the keyboard", () => {
    for (const ev of [
      event({ key: "k", code: "KeyK", ctrlKey: true }),
      event({ key: "Escape", code: "Escape" }),
    ]) {
      expect(matchShortcut(bindings, ev, { isMac: false, scope: "modal" })).toBeNull();
    }
  });
});

describe("bindingAllowedInScope", () => {
  const global: ShortcutBinding = {
    id: "g",
    action: "palette.open",
    combo: "Mod+K",
    section: "general",
    label: "Global",
    when: { global: true },
  };
  const scoped: ShortcutBinding = { ...global, id: "s", when: undefined };

  it("gates on the global flag in editable/terminal only", () => {
    expect(bindingAllowedInScope(scoped, "other")).toBe(true);
    expect(bindingAllowedInScope(scoped, "editable")).toBe(false);
    expect(bindingAllowedInScope(scoped, "terminal")).toBe(false);
    expect(bindingAllowedInScope(global, "editable")).toBe(true);
    expect(bindingAllowedInScope(global, "terminal")).toBe(true);
    expect(bindingAllowedInScope(global, "modal")).toBe(false);
  });

  describe("editableWhenRebound", () => {
    const paneFocus: ShortcutBinding = {
      id: "pane-focus-left",
      action: "pane.focus.left",
      combo: "Mod+Shift+ArrowLeft",
      section: "tabs",
      label: "Focus pane left",
      when: { editableWhenRebound: true },
    };

    it("blocks the default combo in editable, matching the unmarked default", () => {
      expect(bindingAllowedInScope({ ...paneFocus, overridden: false }, "editable")).toBe(false);
      expect(bindingAllowedInScope(paneFocus, "editable")).toBe(false);
    });

    it("allows a rebound combo in editable", () => {
      expect(bindingAllowedInScope({ ...paneFocus, overridden: true }, "editable")).toBe(true);
    });

    it("does not extend to terminal scope -- unlike global, it stays blocked there even when rebound", () => {
      expect(bindingAllowedInScope({ ...paneFocus, overridden: true }, "terminal")).toBe(false);
    });

    it("fires outside editable/terminal regardless of override", () => {
      expect(bindingAllowedInScope({ ...paneFocus, overridden: false }, "other")).toBe(true);
      expect(bindingAllowedInScope({ ...paneFocus, overridden: true }, "other")).toBe(true);
    });

    it("never fires in a modal, rebound or not", () => {
      expect(bindingAllowedInScope({ ...paneFocus, overridden: true }, "modal")).toBe(false);
    });
  });
});

describe("resolveBindings", () => {
  it("applies an override and marks the binding as overridden", () => {
    const resolved = resolveBindings(SHORTCUT_BINDINGS, { "palette-open": "Mod+Shift+P" });
    const palette = resolved.find((b) => b.id === "palette-open")!;
    expect(palette.effectiveCombo).toBe("Mod+Shift+P");
    expect(palette.overridden).toBe(true);

    expect(
      matchShortcut(
        resolved,
        event({ key: "p", code: "KeyP", ctrlKey: true, shiftKey: true }),
        NON_MAC,
      )?.action,
    ).toBe("palette.open");
    // The default no longer fires.
    expect(
      matchShortcut(resolved, event({ key: "k", code: "KeyK", ctrlKey: true }), NON_MAC),
    ).toBeNull();
  });

  it("treats the empty string as unassigned, matching nothing", () => {
    const resolved = resolveBindings(SHORTCUT_BINDINGS, { "palette-open": UNASSIGNED });
    const palette = resolved.find((b) => b.id === "palette-open")!;
    expect(palette.effectiveCombo).toBeNull();
    expect(palette.parsed).toBeNull();
    expect(
      matchShortcut(resolved, event({ key: "k", code: "KeyK", ctrlKey: true }), NON_MAC),
    ).toBeNull();
  });

  it("falls back to the default combo for a garbage override instead of throwing", () => {
    const resolved = resolveBindings(SHORTCUT_BINDINGS, { "palette-open": "Mod+NotAKey" });
    const palette = resolved.find((b) => b.id === "palette-open")!;
    expect(palette.effectiveCombo).toBe("Mod+K");
    expect(palette.overridden).toBe(false);
  });
});

describe("helpSections", () => {
  it("lists every binding, grouped in section order", () => {
    const sections = helpSections(resolveBindings(), false);
    expect(sections.map((s) => s.id)).toEqual(
      SECTION_ORDER.filter((id) => SHORTCUT_BINDINGS.some((b) => b.section === id)),
    );
    const rowIds = sections.flatMap((s) => s.rows.map((r) => r.id));
    expect(rowIds.sort()).toEqual(SHORTCUT_BINDINGS.map((b) => b.id).sort());
  });

  it("renders each row's effective keys for the platform", () => {
    const mac = helpSections(resolveBindings(), true);
    const linux = helpSections(resolveBindings(), false);
    const find = (sections: ReturnType<typeof helpSections>) =>
      sections.flatMap((s) => s.rows).find((r) => r.id === "palette-open")!;
    expect(find(mac).keys).toBe("⌘K");
    expect(find(linux).keys).toBe("Ctrl+K");
  });

  it("shows an unassigned row with null keys rather than dropping it", () => {
    const sections = helpSections(resolveBindings(SHORTCUT_BINDINGS, { "palette-open": UNASSIGNED }), false);
    const row = sections.flatMap((s) => s.rows).find((r) => r.id === "palette-open")!;
    expect(row.keys).toBeNull();
    expect(row.overridden).toBe(true);
  });
});

describe("pane-focus defaults vs. a rebind, in an editable field", () => {
  const paneFocusLeft = event({ key: "ArrowLeft", code: "ArrowLeft", ctrlKey: true, shiftKey: true });

  it("the shipped default (Mod+Shift+ArrowLeft) does not fire in editable scope -- it's the browser's word-select key there", () => {
    const bindings = resolveBindings();
    expect(
      matchShortcut(bindings, paneFocusLeft, { isMac: false, scope: "editable" }),
    ).toBeNull();
  });

  it("the same default still fires outside editable/terminal scope", () => {
    const bindings = resolveBindings();
    expect(
      matchShortcut(bindings, paneFocusLeft, { isMac: false, scope: "other" })?.action,
    ).toBe("pane.focus.left");
  });

  it("a rebind of the same binding does fire in editable scope", () => {
    const bindings = resolveBindings(SHORTCUT_BINDINGS, { "pane-focus-left": "Alt+Shift+H" });
    const rebound = event({ key: "h", code: "KeyH", altKey: true, shiftKey: true });
    expect(
      matchShortcut(bindings, rebound, { isMac: false, scope: "editable" })?.action,
    ).toBe("pane.focus.left");
    // The original default combo no longer matches anything, rebound or not.
    expect(
      matchShortcut(bindings, paneFocusLeft, { isMac: false, scope: "editable" }),
    ).toBeNull();
  });
});

describe("sidebar.task-jump (Alt+Digit) vs. tab.jump (Mod+Alt+Digit)", () => {
  // The web table: the desktop-only Mod+Digit row would match Ctrl+2.
    const bindings = resolveBindings(platformBindings(SHORTCUT_BINDINGS, false, false));
  const NON_MAC_2 = { isMac: false, scope: "other" } as const;

  it("Alt+2 alone fires sidebar.task-jump, not tab.jump", () => {
    const match = matchShortcut(bindings, event({ key: "2", code: "Digit2", altKey: true }), NON_MAC_2);
    expect(match?.action).toBe("sidebar.task-jump");
    expect(match?.payload).toEqual({ digit: 2 });
  });

  it("Ctrl+Alt+2 fires tab.jump, not sidebar.task-jump -- exact-modifier matching keeps the two apart", () => {
    const match = matchShortcut(
      bindings,
      event({ key: "2", code: "Digit2", ctrlKey: true, altKey: true }),
      NON_MAC_2,
    );
    expect(match?.action).toBe("tab.jump");
  });

  it("Ctrl+2 alone (no Alt) fires neither -- not the browser-reserved Mod+Digit this binding deliberately avoids", () => {
    expect(
      matchShortcut(bindings, event({ key: "2", code: "Digit2", ctrlKey: true }), NON_MAC_2),
    ).toBeNull();
  });
});

describe("conflictingBindings", () => {
  it("reports another binding already on the same combo, never the binding itself", () => {
    const resolved = resolveBindings(SHORTCUT_BINDINGS, { "tab-close": "Mod+K" });
    expect(conflictingBindings(resolved, "Mod+K", "tab-close", false).map((b) => b.id)).toEqual([
      "palette-open",
    ]);
    expect(conflictingBindings(resolved, "Mod+K", "palette-open", false).map((b) => b.id)).toEqual([
      "tab-close",
    ]);
    expect(conflictingBindings(resolved, "Mod+Shift+Q", "tab-close", false)).toEqual([]);
  });

  it("sees through platform spelling: a captured Ctrl+K collides with Mod+K on a non-mac", () => {
    const resolved = resolveBindings(SHORTCUT_BINDINGS, { "tab-close": "Ctrl+K" });
    expect(conflictingBindings(resolved, "Ctrl+K", "tab-close", false).map((b) => b.id)).toEqual([
      "palette-open",
    ]);
    // ...and does not collide on a mac, where Mod+K is Cmd+K.
    expect(conflictingBindings(resolved, "Ctrl+K", "tab-close", true)).toEqual([]);
  });

  it("returns nothing for an unparseable combo instead of throwing", () => {
    expect(conflictingBindings(resolveBindings(), "Mod+NotAKey", "tab-close", false)).toEqual([]);
  });
});

describe("resolveChordStep", () => {
  const chordBinding: ShortcutBinding = {
    id: "go-to-settings",
    action: "shortcuts.help",
    combo: "Mod+K S",
    section: "general",
    label: "Chord test binding",
  };
  const plainBinding: ShortcutBinding = {
    id: "palette-open",
    action: "palette.open",
    combo: "Mod+K",
    section: "general",
    label: "Plain binding sharing the chord's first key",
  };
  const bindings = resolveBindings([chordBinding, plainBinding]);

  const step1 = event({ key: "k", code: "KeyK", ctrlKey: true });
  const step2 = event({ key: "s", code: "KeyS" });

  it("waits for the second key instead of firing on the first", () => {
    const result = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    expect(result.match).toBeNull();
    expect(result.pending).toBe(true);
    expect(result.nextChordState.step).toBe(1);
  });

  it("completes on the matching second key", () => {
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    const result = resolveChordStep(bindings, step2, NON_MAC, afterFirst.nextChordState);
    expect(result.match?.action).toBe("shortcuts.help");
    expect(result.pending).toBe(false);
    expect(result.nextChordState).toEqual(INITIAL_CHORD_STATE);
  });

  it("a wrong second key cancels the attempt rather than firing anything", () => {
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    const wrongKey = event({ key: "z", code: "KeyZ" });
    const result = resolveChordStep(bindings, wrongKey, NON_MAC, afterFirst.nextChordState);
    expect(result.match).toBeNull();
    expect(result.nextChordState).toEqual(INITIAL_CHORD_STATE);
  });

  it("the caller's abandon-on-timeout is a plain reset back to the initial state", () => {
    // resolveChordStep has no timers of its own (see its doc comment) --
    // the "waits for the timeout, then cancels" behavior is the caller
    // (keyboard-provider.tsx) resetting chordState back to
    // INITIAL_CHORD_STATE and discarding whatever candidates were pending.
    // Modelled here as: an in-progress attempt, then a fresh event handled
    // against INITIAL_CHORD_STATE, behaves exactly as if no chord had
    // started.
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    expect(afterFirst.nextChordState.step).toBe(1);
    const abandoned: ChordState = INITIAL_CHORD_STATE;
    const result = resolveChordStep(bindings, step2, NON_MAC, abandoned);
    expect(result.match).toBeNull();
  });

  it("survives a re-render: chord state threaded back in from a previous call still completes", () => {
    // The whole point of resolveChordStep being a pure function of its
    // `chordState` argument (not internal state) is that a caller can
    // re-render between the two calls without losing the attempt, as long
    // as it keeps threading `nextChordState` back in -- which is exactly
    // what `keyboard-provider.tsx`'s ref (not React state) does.
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    const carriedOver: ChordState = { ...afterFirst.nextChordState };
    const result = resolveChordStep(bindings, step2, NON_MAC, carriedOver);
    expect(result.match?.action).toBe("shortcuts.help");
  });

  it("a bare modifier keydown mid-chord decides nothing", () => {
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    const bareModifier = event({ key: "Control", code: "ControlLeft", ctrlKey: true });
    const result = resolveChordStep(bindings, bareModifier, NON_MAC, afterFirst.nextChordState);
    expect(result.nextChordState).toEqual(afterFirst.nextChordState);
    expect(result.pending).toBe(false);
    expect(result.match).toBeNull();
  });

  it("ignores auto-repeat mid-chord without disturbing the pending state", () => {
    const afterFirst = resolveChordStep(bindings, step1, NON_MAC, INITIAL_CHORD_STATE);
    const repeated = event({ ...step2, repeat: true });
    const result = resolveChordStep(bindings, repeated, NON_MAC, afterFirst.nextChordState);
    expect(result.match).toBeNull();
    expect(result.nextChordState).toEqual(afterFirst.nextChordState);
  });
});

describe("matchShortcut with a chord binding", () => {
  it("does not fire a chord on its first key alone", () => {
    const bindings = resolveBindings([
      {
        id: "go-to-settings",
        action: "shortcuts.help",
        combo: "Mod+K S",
        section: "general",
        label: "Chord test binding",
      },
    ]);
    expect(
      matchShortcut(bindings, event({ key: "k", code: "KeyK", ctrlKey: true }), NON_MAC),
    ).toBeNull();
  });
});

describe("override migration between single-combo and chord", () => {
  const binding: ShortcutBinding = {
    id: "toggleable",
    action: "shortcuts.help",
    combo: "Mod+K",
    section: "general",
    label: "Migratable binding",
  };

  it("a chord override on a single-combo default still matches its full sequence", () => {
    const resolved = resolveBindings([binding], { toggleable: "Mod+K S" });
    const row = resolved.find((b) => b.id === "toggleable")!;
    expect(row.effectiveCombo).toBe("Mod+K S");
    expect(row.overridden).toBe(true);

    const afterFirst = resolveChordStep(
      resolved,
      event({ key: "k", code: "KeyK", ctrlKey: true }),
      NON_MAC,
      INITIAL_CHORD_STATE,
    );
    expect(afterFirst.pending).toBe(true);
    const result = resolveChordStep(resolved, event({ key: "s", code: "KeyS" }), NON_MAC, afterFirst.nextChordState);
    expect(result.match?.action).toBe("shortcuts.help");
  });

  it("a single-combo override on a chord default resolves on the first key, matching a plain binding", () => {
    const chordDefault: ShortcutBinding = { ...binding, combo: "Mod+K S" };
    const resolved = resolveBindings([chordDefault], { toggleable: "Mod+Shift+K" });
    const row = resolved.find((b) => b.id === "toggleable")!;
    expect(row.effectiveCombo).toBe("Mod+Shift+K");
    expect(
      matchShortcut(
        resolved,
        event({ key: "k", code: "KeyK", ctrlKey: true, shiftKey: true }),
        NON_MAC,
      )?.action,
    ).toBe("shortcuts.help");
  });
});

const code = (c: string, key: string, mods: Partial<KeyEventLike> = {}) =>
  event({ key, code: c, ...mods });
const ctrl = { ctrlKey: true };
const TERM = (isMac: boolean) => ({ isMac, scope: "terminal" }) as const;
const resolved = (isDesktop: boolean, isMac: boolean, overrides = {}) =>
  resolveBindings(platformBindings(SHORTCUT_BINDINGS, isDesktop, isMac), overrides);

describe("terminal scope keeps shell keys", () => {
  it("terminal-scope-ctrl-letters-pass-through", () => {
    const b = resolved(false, false);
    for (const l of ["K", "B", "W", "L", "P", "F"]) {
      expect(matchShortcut(b, code(`Key${l}`, l.toLowerCase(), ctrl), TERM(false))).toBeNull();
      // same keys still fire outside the terminal
      expect(matchShortcut(b, code(`Key${l}`, l.toLowerCase(), ctrl), NON_MAC)).not.toBeNull();
    }
    const rebound = resolved(false, false, { "palette-open": "Ctrl+J" });
    expect(matchShortcut(rebound, code("KeyJ", "j", ctrl), TERM(false))).toBeNull();
    expect(matchShortcut(rebound, code("KeyJ", "j", ctrl), NON_MAC)?.action).toBe("palette.open");

    const chord = resolved(false, false, { "palette-open": "Mod+Shift+K Mod+S" });
    const first = resolveChordStep(chord, code("KeyK", "k", { ...ctrl, shiftKey: true }), TERM(false));
    expect(first.pending).toBe(true);
    const second = resolveChordStep(chord, code("KeyS", "s", ctrl), TERM(false), first.nextChordState);
    expect(second.match).toBeNull();
    const other = resolveChordStep(chord, code("KeyK", "k", { ...ctrl, shiftKey: true }), NON_MAC);
    expect(resolveChordStep(chord, code("KeyS", "s", ctrl), NON_MAC, other.nextChordState).match?.action).toBe("palette.open");
  });

  it("terminal-scope-ctrl-bracket-passes-through", () => {
    const b = resolved(false, false);
    expect(matchShortcut(b, code("BracketLeft", "[", ctrl), TERM(false))).toBeNull();
    expect(matchShortcut(b, code("BracketRight", "]", ctrl), TERM(false))).toBeNull();
    expect(matchShortcut(b, code("Backslash", "\\", ctrl), TERM(false))).toBeNull();
    expect(matchShortcut(b, code("BracketLeft", "[", ctrl), NON_MAC)?.action).toBe("task.prev");
    const mac = resolved(false, true);
    expect(matchShortcut(mac, code("BracketLeft", "[", { metaKey: true }), TERM(true))?.action).toBe("task.prev");
    expect(matchShortcut(mac, code("BracketRight", "]", { metaKey: true }), TERM(true))?.action).toBe("task.next");
    const shift = { ...ctrl, shiftKey: true };
    expect(matchShortcut(b, code("BracketLeft", "{", shift), TERM(false))?.action).toBe("task.prev");
    expect(matchShortcut(b, code("BracketRight", "}", shift), TERM(false))?.action).toBe("task.next");
    expect(matchShortcut(b, code("Backslash", "\\", { altKey: true, shiftKey: true }), TERM(false))?.action).toBe("pane.split.right");
  });

  it("terminal-scope-mac-cmd-still-fires", () => {
    const b = resolved(false, true);
    for (const [c, k, a] of [["KeyK", "k", "palette.open"], ["KeyB", "b", "sidebar.toggle"], ["KeyW", "w", "tab.close"]]) {
      expect(matchShortcut(b, code(c!, k!, { metaKey: true }), TERM(true))?.action).toBe(a);
    }
  });

  it("terminal-scope-ctrl-shift-and-non-letter-still-fire", () => {
    const b = resolved(false, false);
    expect(matchShortcut(b, code("KeyW", "w", { ...ctrl, shiftKey: true }), TERM(false))?.action).toBe("pane.close");
    expect(matchShortcut(b, code("Backslash", "|", { ...ctrl, shiftKey: true }), TERM(false))?.action).toBe("pane.split.down");
    expect(matchShortcut(b, code("Comma", ",", ctrl), TERM(false))?.action).toBe("settings.open");
  });

  it("non-terminal-scopes-ctrl-letters-unchanged", () => {
    const b = resolved(false, false);
    for (const scope of ["other", "editable"] as const) {
      for (const [c, k, a] of [["KeyK", "k", "palette.open"], ["KeyB", "b", "sidebar.toggle"], ["KeyW", "w", "tab.close"]]) {
        expect(matchShortcut(b, code(c!, k!, ctrl), { isMac: false, scope })?.action).toBe(a);
      }
    }
  });

  const HATCH_TABLE: [string, string, KeyEventLike][] = [
    ["palette.open", "palette-open-terminal", code("KeyP", "p", { ...ctrl, shiftKey: true })],
    ["tab.close", "tab-close-terminal", code("KeyW", "w", { altKey: true, shiftKey: true })],
    ["sidebar.toggle", "sidebar-toggle-terminal", code("KeyB", "b", { ...ctrl, shiftKey: true })],
    ["pane.find", "pane-find-terminal", code("KeyF", "f", { ...ctrl, shiftKey: true })],
    ["composer.focus", "composer-focus-terminal", code("KeyL", "l", { ...ctrl, shiftKey: true })],
    ["quick-open.open", "quick-open-open-terminal", code("KeyO", "o", { ...ctrl, shiftKey: true })],
    ["task.prev", "task-prev-terminal", code("BracketLeft", "{", { ...ctrl, shiftKey: true })],
    ["task.next", "task-next-terminal", code("BracketRight", "}", { ...ctrl, shiftKey: true })],
    ["pane.split.right", "pane-split-right-terminal", code("Backslash", "\\", { altKey: true, shiftKey: true })],
    ["tab.new", "tab-new-terminal", code("KeyT", "t", { ...ctrl, shiftKey: true })],
  ];

  it("terminal-escape-hatch-fires-in-terminal", () => {
    for (const [action, id, ev] of HATCH_TABLE) {
      const desktop = id === "tab-new-terminal";
      const m = matchShortcut(resolved(desktop, false), ev, TERM(false));
      expect(m?.action, id).toBe(action);
      expect(m?.binding.id, id).toBe(id);
    }
    // tab.new hatch is desktop-only
    expect(resolved(false, false).some((b) => b.id === "tab-new-terminal")).toBe(false);
  });

  it("escape-hatch-rows-hidden-on-mac", () => {
    for (const isDesktop of [false, true]) {
      expect(resolved(isDesktop, true).some((b) => b.id.endsWith("-terminal"))).toBe(false);
      const ids = resolved(isDesktop, false).map((b) => b.id);
      for (const [, id] of HATCH_TABLE) {
        if (id === "tab-new-terminal" && !isDesktop) expect(ids).not.toContain(id);
        else expect(ids).toContain(id);
      }
    }
  });

  it("no-duplicate-default-combos", () => {
    for (const isMac of [false, true]) {
      for (const isDesktop of [false, true]) {
        const seen = new Map<string, string>();
        for (const b of platformBindings(SHORTCUT_BINDINGS, isDesktop, isMac)) {
          const key = canonicalChord(b.combo, isMac);
          expect(seen.get(key), `${b.id} vs ${seen.get(key)}`).toBeUndefined();
          seen.set(key, b.id);
        }
      }
    }
  });
});
