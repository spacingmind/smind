import { UNASSIGNED, type ShortcutOverrides } from "@/keyboard/shortcuts";

/**
 * Persistence for user rebindings. Same storage shape as
 * `hooks/use-sidebar-width.ts` (one `smind:`-prefixed `localStorage` key,
 * every access guarded) -- the plan's Item 3 calls for one storage
 * mechanism across the shell's preferences, and this is it for keybindings.
 *
 * Paseo's equivalent (`shortcut-override-store.ts`) serializes writes
 * through a promise queue because its storage is an async host RPC that can
 * fail mid-write. `localStorage` is synchronous and either throws
 * immediately or succeeds, so there's no window for two writes to
 * interleave and nothing to roll back -- the queue would be ceremony
 * around a synchronous call.
 */

export const SHORTCUT_OVERRIDES_STORAGE_KEY = "smind:shortcut-overrides";

/** A stored value must be a string: a combo, or {@link UNASSIGNED} for a deliberately-unbound row. */
function isOverrideMap(value: unknown): value is Record<string, string> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return Object.values(value).every((v) => typeof v === "string");
}

/**
 * The persisted override map, or `{}`.
 *
 * Anything unparseable -- absent, malformed JSON, the right JSON of the
 * wrong shape -- reads back as "no overrides" rather than throwing.
 * `resolveBindings` separately tolerates a *well-formed* map holding an
 * unparseable combo, so a single bad entry can't take the others with it.
 */
export function readStoredOverrides(): ShortcutOverrides {
  if (typeof window === "undefined") return {};
  try {
    const raw = window.localStorage.getItem(SHORTCUT_OVERRIDES_STORAGE_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    return isOverrideMap(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

/** Best-effort persistence: a write failure leaves the in-memory overrides applying for the rest of the session. */
export function writeStoredOverrides(overrides: ShortcutOverrides): void {
  if (typeof window === "undefined") return;
  try {
    if (Object.keys(overrides).length === 0) {
      window.localStorage.removeItem(SHORTCUT_OVERRIDES_STORAGE_KEY);
      return;
    }
    window.localStorage.setItem(SHORTCUT_OVERRIDES_STORAGE_KEY, JSON.stringify(overrides));
  } catch {
    // Storage can throw (private browsing, quota, disabled) -- see
    // use-sidebar-width.ts for the same tradeoff.
  }
}

/** Sets `bindingId`'s combo. Pass {@link UNASSIGNED} to unbind it while keeping its row in the help dialog. */
export function setOverride(
  overrides: ShortcutOverrides,
  bindingId: string,
  combo: string,
): ShortcutOverrides {
  return { ...overrides, [bindingId]: combo };
}

/** Drops `bindingId`'s override entirely, restoring its default combo. */
export function clearOverride(overrides: ShortcutOverrides, bindingId: string): ShortcutOverrides {
  if (!Object.prototype.hasOwnProperty.call(overrides, bindingId)) return overrides;
  const next = { ...overrides };
  delete next[bindingId];
  return next;
}

export { UNASSIGNED };
