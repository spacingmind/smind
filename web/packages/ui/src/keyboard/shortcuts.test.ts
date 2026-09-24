import { describe, expect, it } from "vitest";

import {
  bindingAllowedInScope,
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
import type { KeyEventLike } from "@/keyboard/shortcut-string";

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
    const byAction = new Map(SHORTCUT_BINDINGS.map((b) => [b.action, b.combo]));
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
