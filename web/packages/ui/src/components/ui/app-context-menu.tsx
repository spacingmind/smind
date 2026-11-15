import { Slot } from "radix-ui";
import type { ComponentProps, KeyboardEvent, MouseEvent, ReactNode } from "react";

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { MenuEntries, type MenuParts } from "@/components/ui/menu-entries";
import { claimNativeOpen, showNativeContextMenu } from "@/lib/native-context-menu";
import type { MenuEntry } from "@/lib/menu-model";
import { isDesktop } from "@/lib/platform";

/** The context-menu flavour of `MenuParts`. */
export const CONTEXT_MENU_PARTS: MenuParts = {
  Item: ContextMenuItem as MenuParts["Item"],
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
  Separator: ContextMenuSeparator,
};

/**
 * Where a native menu opens for this event: the pointer, or -- for a keyboard-invoked menu (Shift+F10,
 * the Menu key), which some webviews report at 0,0 -- the lower-left corner of the focused element.
 */
export function menuPoint(event: Pick<MouseEvent | KeyboardEvent, "currentTarget"> & { clientX?: number; clientY?: number }): { x: number; y: number } {
  const x = Math.max(0, event.clientX ?? 0);
  const y = Math.max(0, event.clientY ?? 0);
  if ((x !== 0 || y !== 0) || !(event.currentTarget instanceof Element)) return { x, y };
  const rect = event.currentTarget.getBoundingClientRect();
  return { x: Math.max(0, rect.left + 8), y: Math.max(0, rect.bottom) };
}

/**
 * A context menu driven by a menu model (lib/menu-model.ts). In the browser build it is exactly the Radix
 * menu the app has always had (same wrappers, test ids and behaviour). In the desktop build the trigger's
 * `contextmenu` event pops a NATIVE menu from the same model instead, and the Radix content is never
 * rendered. `onOpenChange` is called for both flavours (the native one reports open on show and closed when
 * the native menu returns, or if showing it fails).
 *
 * `children` is the one trigger element (as with `ContextMenuTrigger asChild`).
 */
export function AppContextMenu({
  entries,
  children,
  contentProps,
  onOpenChange,
  onDialogSelect,
}: {
  entries: readonly MenuEntry[];
  children: ReactNode;
  contentProps?: Omit<ComponentProps<typeof ContextMenuContent>, "children"> & Record<`data-${string}`, string>;
  onOpenChange?: (open: boolean) => void;
  onDialogSelect?: () => void;
}) {
  if (isDesktop) {
    const open = (event: MouseEvent<HTMLElement> | KeyboardEvent<HTMLElement>) => {
      // Everything above this trigger (a parent row's menu, the document policy) must not also act.
      event.preventDefault();
      event.stopPropagation();
      // A menu with nothing in it would only be refused natively; and a repeat open within a blink (a key repeat,
      // a double right-click) must not queue a second blocking menu.
      if (!entries.some((e) => e.kind === "item" || e.kind === "submenu")) return;
      if (!claimNativeOpen()) return;
      void showNativeContextMenu(entries, menuPoint(event), { onOpenChange }).catch((err: unknown) => {
        console.warn("native context menu failed", err);
      });
    };
    return (
      <Slot.Root
        onContextMenu={open}
        onKeyDown={(event: KeyboardEvent<HTMLElement>) => {
          // The keyboard routes a webview may not turn into a `contextmenu` event itself: the Menu key and Shift+F10, on the focused trigger.
          if (event.target === event.currentTarget && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) open(event);
        }}
      >
        {children}
      </Slot.Root>
    );
  }
  return (
    <ContextMenu onOpenChange={onOpenChange}>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent {...contentProps}>
        <MenuEntries entries={entries} parts={CONTEXT_MENU_PARTS} onDialogSelect={onDialogSelect} />
      </ContextMenuContent>
    </ContextMenu>
  );
}
