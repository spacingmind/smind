import { useMemo } from "react";

import type { ActionId } from "@/keyboard/actions";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import type { IconName } from "@/lib/icons";
import { type DesktopOS, type NativeMenuActionId, desktop, desktopOS, isDesktop } from "@/lib/platform";
import type { Command } from "@/palette/commands";
import { useCommands } from "@/palette/palette-provider";

/**
 * The application menu's items as palette rows (ui-cleanup-b1 H1). The
 * native ones call the same `desktop.menuAction(id)` the header hamburger
 * and the native menu bar dispatch; the renderer ones run the same
 * keyboard-registry actions with `source: "menu"` so a focus-gated handler
 * still receives them (opening the palette moved focus out of its pane).
 * Settings and the palette itself are already palette routes, so they are
 * not repeated here.
 */

/** What `buildDesktopCommands` needs; pure data in, `Command[]` out, so tests pass plain stubs. */
export interface DesktopCommandDeps {
  isDesktop: boolean;
  os: DesktopOS | null;
  /** Runs a native-only app-menu item (`app_menu_action`). */
  menuAction: (id: NativeMenuActionId) => Promise<void>;
  /** Runs a renderer action the way a menu click does. */
  runAction: (action: ActionId, payload?: null, options?: { source: "menu" }) => boolean;
}

/** A native menu item, keyed by the id `app_menu_action` accepts. */
interface NativeEntry {
  id: NativeMenuActionId;
  title: string;
  /** The keyboard action whose shortcut the row shows, when one exists. */
  action?: ActionId;
  icon?: IconName;
  keywords?: readonly string[];
  /** Windows only: there is no Linux updater (mirrors the native Help menu's gate). */
  windowsOnly?: boolean;
}

const NATIVE_ENTRIES: readonly NativeEntry[] = [
  { id: "reload", title: "Reload", icon: "refresh", keywords: ["refresh"] },
  { id: "zoom-in", title: "Zoom in", keywords: ["magnify", "bigger"] },
  { id: "zoom-out", title: "Zoom out", keywords: ["smaller", "shrink"] },
  { id: "zoom-reset", title: "Reset zoom", icon: "reset", keywords: ["actual size"] },
  { id: "fullscreen", title: "Toggle fullscreen", keywords: ["full screen"] },
  { id: "minimize", title: "Minimize" },
  { id: "hide", title: "Hide window", keywords: ["tray"] },
  { id: "about", title: "About sMind", icon: "info" },
  { id: "log-folder", title: "Open log folder", icon: "folderOpen", keywords: ["logs", "diagnostics"] },
  { id: "github", title: "sMind on GitHub", icon: "externalLink", keywords: ["source", "issues"] },
  // The renderer owns Ctrl+Q here (`app-quit` in keyboard/shortcuts.ts).
  { id: "quit", title: "Quit sMind", action: "app.quit", keywords: ["exit"] },
  { id: "check-updates", title: "Check for updates", icon: "download", keywords: ["update", "upgrade"], windowsOnly: true },
];

/** The renderer actions the application menu also listed. */
const RENDERER_ACTIONS: readonly { action: ActionId; title: string; icon?: IconName; keywords?: readonly string[] }[] = [
  { action: "sidebar.toggle", title: "Toggle sidebar", icon: "sidebarToggle" },
  { action: "pane.find", title: "Find in pane", icon: "search", keywords: ["search"] },
  { action: "tab.new", title: "New tab", icon: "add" },
  { action: "tab.close", title: "Close tab", icon: "close" },
];

/**
 * The palette entries for the application menu's items. Desktop only
 * (every desktop OS -- the palette is the one place the macOS native
 * menu's items are keyboard-searchable); `[]` on web.
 */
export function buildDesktopCommands(deps: DesktopCommandDeps): Command[] {
  if (!deps.isDesktop) return [];
  const native = NATIVE_ENTRIES.filter((entry) => !entry.windowsOnly || deps.os === "windows").map(
    (entry): Command => ({
      id: `desktop-${entry.id}`,
      group: "Window",
      title: entry.title,
      ...(entry.keywords === undefined ? {} : { keywords: entry.keywords }),
      ...(entry.action === undefined ? {} : { action: entry.action }),
      ...(entry.icon === undefined ? {} : { icon: entry.icon }),
      run: () => {
        void deps.menuAction(entry.id).catch(() => {});
      },
    }),
  );
  const renderer = RENDERER_ACTIONS.map(
    (entry): Command => ({
      id: `desktop-${entry.action}`,
      group: "App",
      title: entry.title,
      ...(entry.keywords === undefined ? {} : { keywords: entry.keywords }),
      action: entry.action,
      ...(entry.icon === undefined ? {} : { icon: entry.icon }),
      run: () => {
        deps.runAction(entry.action, null, { source: "menu" });
      },
    }),
  );
  return [...native, ...renderer];
}

/** Registers the desktop entries as palette source `shell:desktop` (rank 6). */
export function useDesktopCommands(): void {
  const { runAction } = useKeyboard();
  const commands = useMemo(
    () =>
      buildDesktopCommands({
        isDesktop,
        os: desktopOS,
        menuAction: (id) => desktop.menuAction(id),
        runAction,
      }),
    [runAction],
  );
  useCommands("shell:desktop", 6, commands);
}
