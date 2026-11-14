import type { ReactNode } from "react";

/**
 * The menu model (desktop-native-feel DT2 (a)): every context menu is DATA, built by one pure function
 * per menu and rendered by two renderers that cannot drift -- the in-page Radix menu (the browser build
 * and the fallback) and the native menu (the desktop build, through `lib/native-context-menu.ts`). The
 * same pure functions are what the per-menu table tests exercise, including each item's enabled state.
 *
 * What crosses to the native side is labels, opaque ids and enabled flags only (`toNative`); icons, test
 * ids, notes and handlers stay in the renderer.
 */

export interface MenuItemEntry {
  kind: "item";
  /** Unique within its menu: an opaque key, never a path or a command. */
  id: string;
  label: string;
  onSelect: () => void;
  /** False greys the item out. Default true. */
  enabled?: boolean;
  testId?: string;
  /** In-page menu only. */
  icon?: ReactNode;
  /**
   * Why an unavailable item is unavailable ("Archive: still running"). In the page it is shown beside the
   * label and keeps the item keyboard-reachable (lib/menu-item-disabled.ts); natively it is appended to
   * the label in parentheses.
   */
  note?: string;
  /** An unavailable item that stays keyboard-reachable (its reason is the `note`): lib/menu-item-disabled.ts. Default: reachable exactly when it has a note and a test id. */
  reachable?: boolean;
  /** The item opens a dialog: the in-page menu closes with no exit animation under it. */
  opensDialog?: boolean;
  /** Extra data-* attributes for the in-page item (tests and callers that disambiguate items). */
  data?: Record<string, string | number>;
}

export interface MenuSeparatorEntry {
  kind: "separator";
}

export interface MenuSubmenuEntry {
  kind: "submenu";
  id: string;
  label: string;
  enabled?: boolean;
  testId?: string;
  icon?: ReactNode;
  children: Array<MenuItemEntry | MenuSeparatorEntry>;
}

export type MenuEntry = MenuItemEntry | MenuSeparatorEntry | MenuSubmenuEntry;

/** What the native bridge sends (mirrors desktop/src-tauri/src/context_menu.rs `MenuEntry`). */
export type NativeMenuEntry =
  | { kind: "item"; id: string; label: string; enabled: boolean }
  | { kind: "separator" }
  | { kind: "submenu"; label: string; enabled: boolean; children: NativeMenuEntry[] };

/** Drops leading, trailing and doubled separators and empty submenus, recursively, so a model never renders a dangling rule. */
export function normalizeEntries<T extends MenuEntry>(entries: readonly T[]): T[] {
  const cleaned: T[] = [];
  for (const entry of entries) {
    let next: T = entry;
    if (entry.kind === "submenu") {
      const children = normalizeEntries(entry.children);
      if (children.every((c) => c.kind === "separator")) continue;
      next = { ...entry, children } as T;
    }
    if (next.kind === "separator" && (cleaned.length === 0 || cleaned[cleaned.length - 1].kind === "separator")) continue;
    cleaned.push(next);
  }
  while (cleaned.length > 0 && cleaned[cleaned.length - 1].kind === "separator") cleaned.pop();
  return cleaned;
}

/** Every actionable item of a menu, flattened (submenu items included), in order. */
export function itemsOf(entries: readonly MenuEntry[]): MenuItemEntry[] {
  return entries.flatMap((e) => (e.kind === "item" ? [e] : e.kind === "submenu" ? e.children.filter((c): c is MenuItemEntry => c.kind === "item") : []));
}

/** The ids of the enabled items, in order: the shape the per-menu state tables assert on. */
export function enabledIds(entries: readonly MenuEntry[]): string[] {
  return itemsOf(entries)
    .filter((i) => i.enabled !== false)
    .map((i) => i.id);
}

/** The ids of every item, in order, separators written as "-" and a submenu as "id>[...]": a compact picture of a menu for table tests. */
export function outline(entries: readonly MenuEntry[]): string[] {
  return entries.flatMap((e): string[] => {
    if (e.kind === "separator") return ["-"];
    if (e.kind === "item") return [e.enabled === false ? `${e.id}(off)` : e.id];
    return [`${e.id}>[${outline(e.children).join(",")}]`];
  });
}

/** Native entries plus the handler of each item id. A disabled item gets no handler: the native menu cannot select it, and a forged selection finds nothing to run. */
export function toNative(entries: readonly MenuEntry[]): { entries: NativeMenuEntry[]; handlers: Map<string, () => void> } {
  const handlers = new Map<string, () => void>();
  const convert = (list: readonly MenuEntry[]): NativeMenuEntry[] =>
    list.map((e): NativeMenuEntry => {
      if (e.kind === "separator") return { kind: "separator" };
      if (e.kind === "submenu") return { kind: "submenu", label: e.label, enabled: e.enabled !== false, children: convert(e.children) };
      const enabled = e.enabled !== false;
      if (enabled) handlers.set(e.id, e.onSelect);
      return { kind: "item", id: e.id, label: e.note ? `${e.label} (${e.note})` : e.label, enabled };
    });
  return { entries: convert(normalizeEntries(entries)), handlers };
}
