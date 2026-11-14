import { desktop, isDesktop } from "@/lib/platform";
import { toNative, type MenuEntry } from "@/lib/menu-model";

/**
 * The renderer side of the native context menu (DT2 (a)). `showNativeContextMenu` sends a menu model's
 * descriptors to Rust (`context_menu_show`), arms the item handlers, and runs the chosen one when Rust
 * answers with a `context-menu-action` event. Only the most recently opened menu is armed; a selection for
 * anything else (an earlier menu, an id the armed menu does not carry, a second delivery) is ignored.
 * Tauri reports no dismissal, so an armed menu is cleared by the next open, by its selection, or after
 * `ARMED_TTL_MS`.
 */

/** Matches Rust's `ARMED_TTL`: past it the native side no longer delivers a selection. */
export const ARMED_TTL_MS = 30_000;

interface Armed {
  /** Rust's open id, once `context_menu_show` has answered (the selection can beat that answer). */
  open: number | null;
  handlers: Map<string, () => void>;
  onClose?: () => void;
  timer: ReturnType<typeof setTimeout>;
}

let armed: Armed | null = null;
let unlisten: (() => void) | null = null;

function disarm(): void {
  if (!armed) return;
  clearTimeout(armed.timer);
  const closing = armed;
  armed = null;
  closing.onClose?.();
}

function onAction(payload: { open: number; id: string }): void {
  const current = armed;
  if (!current) return;
  if (current.open !== null && current.open !== payload.open) return;
  const handler = current.handlers.get(payload.id);
  if (!handler) return;
  disarm();
  handler();
}

function ensureListening(): void {
  if (unlisten) return;
  unlisten = desktop.onContextMenuAction(onAction);
}

/** Opens the native menu for `entries` at the pointer (`x`, `y` in client pixels). Resolves when Rust has shown it. */
export async function showNativeContextMenu(
  entries: readonly MenuEntry[],
  point: { x: number; y: number },
  opts: { onOpenChange?: (open: boolean) => void } = {},
): Promise<void> {
  if (!isDesktop) throw new Error("native context menus exist only in the desktop build");
  const { entries: native, handlers } = toNative(entries);
  disarm();
  ensureListening();
  const mine: Armed = {
    open: null,
    handlers,
    onClose: () => opts.onOpenChange?.(false),
    timer: setTimeout(() => {
      if (armed === mine) disarm();
    }, ARMED_TTL_MS),
  };
  armed = mine;
  opts.onOpenChange?.(true);
  try {
    const open = await desktop.contextMenuShow(native, point.x, point.y);
    if (armed === mine) mine.open = open;
  } catch (err) {
    if (armed === mine) disarm();
    throw err;
  }
}

/** Test hook: forget the armed menu and the listener. */
export function resetNativeContextMenuForTests(): void {
  if (armed) clearTimeout(armed.timer);
  armed = null;
  unlisten?.();
  unlisten = null;
}
