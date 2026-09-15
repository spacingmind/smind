import type { ActionId, ActionPayload, FocusScope } from "@/keyboard/actions";
import {
  canonicalCombo,
  DIGIT_WILDCARD,
  formatCombo,
  matchCombo,
  parseCombo,
  type KeyCombo,
  type KeyEventLike,
} from "@/keyboard/shortcut-string";

/**
 * The binding table: which combo fires which action, under what focus
 * conditions, and how it reads in the shortcuts dialog.
 *
 * Shape follows `audit-paseo.md` §7 and
 * `refs/paseo/packages/app/src/keyboard/keyboard-shortcuts.ts`, with one
 * deliberate simplification: Paseo writes every cross-platform shortcut
 * *twice* (a `Cmd+K` row gated `{mac: true}` and a `Ctrl+K` row gated
 * `{mac: false}`), because its rebinding UI wants each platform's row
 * independently overridable. smind uses the single `Mod` token instead --
 * one row, resolved to Cmd or Ctrl at match time -- which halves the table
 * and means a user who rebinds on their laptop gets the same binding on
 * their desktop. The tradeoff is that a platform-specific override isn't
 * expressible; nothing in smind needs one today, and `Cmd`/`Ctrl` remain
 * spellable for a binding that genuinely is one-platform-only.
 */

export type SectionId = "general" | "navigation" | "tabs" | "agent";

/** Sections render in this order in the help dialog -- most-reached-for first. */
export const SECTION_ORDER: readonly SectionId[] = ["general", "navigation", "tabs", "agent"];

export const SECTION_TITLES: Record<SectionId, string> = {
  general: "General",
  navigation: "Navigation",
  tabs: "Tabs & panes",
  agent: "Agent",
};

/** Focus conditions a binding opts out of. Absent means the default: blocked in `editable` and `terminal`. */
export interface BindingWhen {
  /**
   * `true` = fires regardless of focus scope (except `modal`, which no
   * binding escapes -- an open dialog owns the keyboard). This is the
   * plan's "except where explicitly marked global".
   */
  global?: true;
}

export interface ShortcutBinding {
  /** Stable across combo changes -- overrides are keyed by it, so renaming one silently drops a user's rebind. */
  id: string;
  action: ActionId;
  /** Default combo, in `shortcut-string.ts`'s spelling. */
  combo: string;
  section: SectionId;
  /** Sentence-case, imperative, no trailing period -- `docs/design.md` §5. */
  label: string;
  when?: BindingWhen;
  /** Extra context shown under the label in the help dialog. */
  note?: string;
}

/**
 * Initial bindings, matching Paseo's defaults wherever the action exists
 * there (verified against its own table: `Cmd+B` sidebar, `Cmd+K` command
 * center, `Cmd+L` message input, `Cmd+W` close tab, `Cmd+Alt+Digit` jump
 * to tab, `Cmd+[`/`Cmd+]` previous/next, `Cmd+Alt+T` cycle theme,
 * `Shift+?` shortcuts dialog).
 *
 * `Escape` for interrupt is smind's own choice, per the plan -- Paseo
 * binds interrupt to a chord smind's chord-less matcher can't express.
 */
export const SHORTCUT_BINDINGS: readonly ShortcutBinding[] = [
  {
    id: "palette-open",
    action: "palette.open",
    combo: "Mod+K",
    section: "general",
    label: "Open command palette",
    when: { global: true },
  },
  {
    id: "shortcuts-help",
    action: "shortcuts.help",
    combo: "Shift+?",
    section: "general",
    label: "Show keyboard shortcuts",
  },
  {
    id: "theme-cycle",
    action: "theme.cycle",
    combo: "Mod+Alt+T",
    section: "general",
    label: "Cycle theme",
    note: "Light \u2192 dark \u2192 system",
  },
  {
    id: "sidebar-toggle",
    action: "sidebar.toggle",
    combo: "Mod+B",
    section: "navigation",
    label: "Toggle sidebar",
    when: { global: true },
  },
  {
    id: "task-prev",
    action: "task.prev",
    combo: "Mod+[",
    section: "navigation",
    label: "Previous task",
    when: { global: true },
  },
  {
    id: "task-next",
    action: "task.next",
    combo: "Mod+]",
    section: "navigation",
    label: "Next task",
    when: { global: true },
  },
  {
    id: "tab-close",
    action: "tab.close",
    combo: "Mod+W",
    section: "tabs",
    label: "Close current tab",
    when: { global: true },
  },
  {
    id: "tab-jump",
    action: "tab.jump",
    combo: "Mod+Alt+Digit",
    section: "tabs",
    label: "Jump to tab by number",
    when: { global: true },
  },
  {
    id: "composer-focus",
    action: "composer.focus",
    combo: "Mod+L",
    section: "agent",
    label: "Focus the composer",
    when: { global: true },
  },
  {
    id: "quick-open-open",
    action: "quick-open.open",
    combo: "Mod+P",
    section: "navigation",
    label: "Quick open a file",
    when: { global: true },
  },
  {
    id: "run-interrupt",
    action: "run.interrupt",
    combo: "Escape",
    section: "agent",
    label: "Interrupt the running agent",
    when: { global: true },
    note: "Works while typing in the composer",
  },
];


/**
 * User rebindings, keyed by binding id. A value of {@link UNASSIGNED} means
 * "this binding matches nothing" -- distinct from an absent key, which
 * means "use the default combo".
 */
export type ShortcutOverrides = Readonly<Record<string, string>>;

/** The stored value for a binding the user has deliberately unassigned. */
export const UNASSIGNED = "";

/** A binding with its effective combo resolved and parsed. `combo`/`parsed` are null when unassigned. */
export interface ResolvedBinding extends ShortcutBinding {
  /** The combo actually in effect: the override if there is one, else the default. */
  effectiveCombo: string | null;
  parsed: KeyCombo | null;
  /** True when the effective combo differs from what the binding shipped with. */
  overridden: boolean;
}

/**
 * Applies overrides to the binding table.
 *
 * A malformed or unknown-key override is *ignored* rather than thrown on:
 * it comes from `localStorage`, which a stale build or a hand-edit can
 * leave garbage in, and a hard failure there would take the whole app down
 * over a keyboard preference. The binding falls back to its default combo,
 * which is the behavior a user who broke their own storage would expect.
 */
export function resolveBindings(
  bindings: readonly ShortcutBinding[] = SHORTCUT_BINDINGS,
  overrides: ShortcutOverrides = {},
): ResolvedBinding[] {
  return bindings.map((binding) => {
    const override = Object.prototype.hasOwnProperty.call(overrides, binding.id)
      ? overrides[binding.id]
      : undefined;

    if (override === UNASSIGNED) {
      return { ...binding, effectiveCombo: null, parsed: null, overridden: true };
    }

    const combo = override ?? binding.combo;
    try {
      return {
        ...binding,
        effectiveCombo: combo,
        parsed: parseCombo(combo),
        overridden: combo !== binding.combo,
      };
    } catch {
      return {
        ...binding,
        effectiveCombo: binding.combo,
        parsed: parseCombo(binding.combo),
        overridden: false,
      };
    }
  });
}

/** Whether a binding is allowed to fire with focus where it currently is. */
export function bindingAllowedInScope(binding: ShortcutBinding, scope: FocusScope): boolean {
  // No binding fires inside a modal: a dialog owns its own keyboard, down
  // to Escape (which closes it rather than interrupting a run).
  if (scope === "modal") return false;
  if (scope === "editable" || scope === "terminal") return binding.when?.global === true;
  return true;
}

export interface ShortcutMatch {
  binding: ResolvedBinding;
  action: ActionId;
  payload: ActionPayload;
}

/**
 * The first binding this event fires, or null.
 *
 * Auto-repeat is dropped outright: every action here is a discrete command
 * (open a palette, close a tab), and holding the key down should do it once.
 */
export function matchShortcut(
  bindings: readonly ResolvedBinding[],
  event: KeyEventLike,
  context: { isMac: boolean; scope: FocusScope },
): ShortcutMatch | null {
  if (event.repeat) return null;

  for (const binding of bindings) {
    if (binding.parsed === null) continue;
    if (!bindingAllowedInScope(binding, context.scope)) continue;
    const match = matchCombo(binding.parsed, event, context.isMac);
    if (match === null) continue;
    return {
      binding,
      action: binding.action,
      payload: match.digit === undefined ? null : { digit: match.digit },
    };
  }
  return null;
}

export interface HelpRow {
  id: string;
  label: string;
  note?: string;
  /** How the effective combo reads on this platform, or null when unassigned. */
  keys: string | null;
  overridden: boolean;
}

export interface HelpSection {
  id: SectionId;
  title: string;
  rows: HelpRow[];
}

/**
 * Every binding, grouped by section in {@link SECTION_ORDER}, for the help
 * dialog and (later) a settings rebinding list.
 *
 * Rows keep binding-table order within their section rather than being
 * re-sorted: the table is already written grouped by intent, so a separate
 * explicit row order (which Paseo maintains, and has to test for
 * completeness) would be a second list to keep in sync for no gain at
 * smind's binding count.
 */
export function helpSections(bindings: readonly ResolvedBinding[], isMac: boolean): HelpSection[] {
  return SECTION_ORDER.map((id) => ({
    id,
    title: SECTION_TITLES[id],
    rows: bindings
      .filter((b) => b.section === id)
      .map((b) => ({
        id: b.id,
        label: b.label,
        ...(b.note === undefined ? {} : { note: b.note }),
        keys: b.effectiveCombo === null ? null : formatCombo(b.effectiveCombo, isMac),
        overridden: b.overridden,
      })),
  })).filter((section) => section.rows.length > 0);
}

/**
 * Bindings whose effective combo collides with `combo` on this platform --
 * what a rebinding UI shows as "already used by X".
 *
 * Comparison is on the *canonical* combo, not the literal string: a
 * binding captured from a real key press spells itself `Ctrl+K`, while the
 * shipped table spells the same shortcut `Mod+K`, and those must be
 * recognised as the same thing on a non-mac. `exceptId` is the binding
 * being edited, which never conflicts with itself.
 */
export function conflictingBindings(
  bindings: readonly ResolvedBinding[],
  combo: string,
  exceptId: string,
  isMac: boolean,
): ResolvedBinding[] {
  let target: string;
  try {
    target = canonicalCombo(combo, isMac);
  } catch {
    return [];
  }
  return bindings.filter((b) => {
    if (b.id === exceptId || b.effectiveCombo === null) return false;
    try {
      return canonicalCombo(b.effectiveCombo, isMac) === target;
    } catch {
      return false;
    }
  });
}

export { DIGIT_WILDCARD };
