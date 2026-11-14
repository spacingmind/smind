import { desktop, isDesktop } from "@/lib/platform";
import { toNative, type MenuEntry } from "@/lib/menu-model";

/**
 * The renderer side of the native context menu (DT2 (a)). `showNativeContextMenu` sends a menu model's
 * descriptors to Rust (`context_menu_show`), arms the item handlers, and runs the chosen one when Rust
 * answers with a `context-menu-action` event. Only the most recently opened menu is armed; a selection for
 * anything else (an earlier menu, an id the armed menu does not carry, a second delivery) is ignored.
 * Tauri reports no dismissal event, but `context_menu_show` returns when the native menu closes (macOS,
 * Windows), which is when `onOpenChange(false)` runs. The handlers stay armed past that (the selection event
 * can arrive just after the return) until the next open, the selection, or `ARMED_TTL_MS`.
 */

/** Matches Rust's `ARMED_TTL`: past it the native side no longer delivers a selection. */
export const ARMED_TTL_MS = 600_000;

interface Armed {
  /** Rust's open id, once `context_menu_show` has answered (the selection can beat that answer). */
  open: number | null;
  handlers: Map<string, () => void>;
  timer: ReturnType<typeof setTimeout>;
}

let armed: Armed | null = null;
let listening: Promise<() => void> | null = null;

function disarm(): void {
  if (!armed) return;
  clearTimeout(armed.timer);
  armed = null;
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

/** Resolves once the native listener is registered, so a selection can never fire before it. Retries after a failure. */
function ensureListening(): Promise<unknown> {
  if (!listening) {
    listening = desktop.onContextMenuAction(onAction);
    listening.catch(() => {
      listening = null;
    });
  }
  return listening;
}

/** Opens the native menu for `entries` at the pointer (`x`, `y` in client pixels). Resolves when the native menu has closed. */
export async function showNativeContextMenu(
  entries: readonly MenuEntry[],
  point: { x: number; y: number },
  opts: { onOpenChange?: (open: boolean) => void } = {},
): Promise<void> {
  if (!isDesktop) throw new Error("native context menus exist only in the desktop build");
  const { entries: native, handlers } = toNative(entries);
  disarm();
  await ensureListening();
  const mine: Armed = {
    open: null,
    handlers,
    timer: setTimeout(() => {
      if (armed === mine) disarm();
    }, ARMED_TTL_MS),
  };
  armed = mine;
  opts.onOpenChange?.(true);
  try {
    const open = await desktop.contextMenuShow(native, point.x, point.y);
    if (armed === mine) mine.open = open;
    // The native menu has closed: the page's "open" state ends, the handlers stay armed for a late selection.
    opts.onOpenChange?.(false);
  } catch (err) {
    if (armed === mine) disarm();
    opts.onOpenChange?.(false);
    throw err;
  }
}

const REOPEN_MS = 100;
let lastOpenAt = 0;

/** False for an open that follows the previous one within a blink (key repeat, double right-click): it must not queue a second blocking menu. */
export function claimNativeOpen(now: number = Date.now()): boolean {
  if (now - lastOpenAt < REOPEN_MS) return false;
  lastOpenAt = now;
  return true;
}

/** Test hook: forget the armed menu and the listener. */
export function resetNativeContextMenuForTests(): void {
  if (armed) clearTimeout(armed.timer);
  armed = null;
  lastOpenAt = 0;
  void listening?.then((fn) => fn()).catch(() => {});
  listening = null;
}
