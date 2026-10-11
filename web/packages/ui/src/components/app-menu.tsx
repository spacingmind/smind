import { Fragment, useRef } from "react";
import { Menu } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { ActionId } from "@/keyboard/actions";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import { desktop, desktopOS, isDesktop, type NativeMenuActionId } from "@/lib/platform";
import { hasCaptionButtons } from "@/lib/window-chrome";

/**
 * An app-menu entry: either a renderer action, dispatched through the same
 * keyboard registry a native menu click or a keystroke uses (its `action` id
 * equals `menu.rs`'s `menu_action_for` string), or a native-only item reached
 * through the scoped `app_menu_action` command.
 */
type AppMenuItem = { label: string; testId: string; shortcut?: string } & (
  { action: ActionId } | { native: NativeMenuActionId }
);

/** Sections mirror the native menu's File / View / Window / Help. */
export const APP_MENU_SECTIONS: { title: string; items: AppMenuItem[] }[] = [
  {
    title: "File",
    items: [
      { label: "New Tab", testId: "tab.new", action: "tab.new" },
      { label: "Close Tab", testId: "tab.close", action: "tab.close" },
      { label: "Settings…", testId: "settings.open", action: "settings.open" },
      { label: "Reload", testId: "reload", native: "reload" },
      // Honoured: the renderer owns Ctrl+Q here (`app-quit` in keyboard/shortcuts.ts).
      { label: "Quit", testId: "quit", native: "quit", shortcut: "Ctrl+Q" },
    ],
  },
  {
    title: "View",
    items: [
      { label: "Command Palette", testId: "palette.open", action: "palette.open" },
      { label: "Find", testId: "pane.find", action: "pane.find" },
      { label: "Toggle Sidebar", testId: "sidebar.toggle", action: "sidebar.toggle" },
      { label: "Zoom In", testId: "zoom-in", native: "zoom-in" },
      { label: "Zoom Out", testId: "zoom-out", native: "zoom-out" },
      { label: "Reset Zoom", testId: "zoom-reset", native: "zoom-reset" },
      { label: "Toggle Fullscreen", testId: "fullscreen", native: "fullscreen" },
    ],
  },
  {
    title: "Window",
    items: [
      { label: "Minimize", testId: "minimize", native: "minimize" },
      { label: "Hide to Tray", testId: "hide", native: "hide" },
    ],
  },
  {
    title: "Help",
    items: [
      { label: "About smind", testId: "about", native: "about" },
      { label: "Open Log Folder", testId: "log-folder", native: "log-folder" },
      { label: "smind on GitHub", testId: "github", native: "github" },
    ],
  },
];

/**
 * The header's hamburger menu (Windows/Linux desktop only). The window is
 * undecorated there, so the native menu bar isn't drawn; this carries the
 * same File/View/Window/Help actions. Accelerator text is shown only where
 * the renderer provably honours it (Quit's Ctrl+Q) -- never for combos that
 * only the hidden native menu would carry.
 */
export function AppMenu() {
  const { runAction } = useKeyboard();
  // The chosen item runs once the menu has closed (see onCloseAutoFocus below).
  const pending = useRef<AppMenuItem | null>(null);
  if (!isDesktop || !hasCaptionButtons(desktopOS)) return null;

  function run(item: AppMenuItem) {
    pending.current = null;
    // `source: "menu"` so Find reaches the last focused pane (opening this
    // menu moved focus out of it).
    if ("action" in item) runAction(item.action, null, { source: "menu" });
    else desktop.menuAction(item.native).catch(() => {});
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Application menu"
          data-testid="app-menu-trigger"
        >
          <Menu />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        className="min-w-48"
        data-testid="app-menu"
        // Radix refocuses the trigger when the menu closes, which would steal focus
        // from whatever the chosen item focuses (the find bar `pane.find` opens). So
        // skip that, and run the item only now: while the menu is open its focus trap
        // would also pull the freshly focused find bar back.
        onCloseAutoFocus={(e) => {
          e.preventDefault();
          const item = pending.current;
          if (item) run(item);
        }}
      >
        {APP_MENU_SECTIONS.map((section, index) => (
          <Fragment key={section.title}>
            {index > 0 && <DropdownMenuSeparator />}
            <DropdownMenuLabel className="py-1 text-ui-sm text-muted-foreground">
              {section.title}
            </DropdownMenuLabel>
            {section.items.map((item) => (
              <DropdownMenuItem
                key={item.testId}
                data-testid={`app-menu-item-${item.testId}`}
                onSelect={() => {
                  pending.current = item;
                }}
              >
                {item.label}
                {item.shortcut && (
                  <span className="ml-auto pl-4 text-ui-sm text-muted-foreground">
                    {item.shortcut}
                  </span>
                )}
              </DropdownMenuItem>
            ))}
          </Fragment>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
