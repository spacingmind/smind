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
import { showNativeContextMenu } from "@/lib/native-context-menu";
import type { MenuEntry } from "@/lib/menu-model";
import { isDesktop } from "@/lib/platform";

/** The context-menu flavour of `MenuParts`. */
export const CONTEXT_MENU_PARTS: MenuParts = {
  Item: ContextMenuItem as MenuParts["Item"],
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
  Separator: ContextMenuSeparator as MenuParts["Separator"],
};

/**
 * Where a native menu opens for this event: the pointer, or -- for a keyboard-invoked menu (Shift+F10,
 * the Menu key), which some webviews report at 0,0 -- the lower-left corner of the focused element.
 */
export function menuPoint(event: Pick<MouseEvent | KeyboardEvent, "currentTarget"> & { clientX?: number; clientY?: number }): { x: number; y: number } {
  const x = event.clientX ?? 0;
  const y = event.clientY ?? 0;
  if ((x !== 0 || y !== 0) || !(event.currentTarget instanceof Element)) return { x, y };
  const rect = event.currentTarget.getBoundingClientRect();
  return { x: rect.left + 8, y: rect.bottom };
}

/**
 * A context menu driven by a menu model (lib/menu-model.ts). In the browser build it is exactly the Radix
 * menu the app has always had (same wrappers, test ids and behaviour). In the desktop build the trigger's
 * `contextmenu` event pops a NATIVE menu from the same model instead, and the Radix content is never
 * rendered. `onOpenChange` is called for both flavours (the native one reports open on show and closed on
 * selection, the next open, or its time-out: Tauri does not report a dismissal).
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
    return (
      <Slot.Root
        onContextMenu={(event: MouseEvent<HTMLElement>) => {
          // Everything above this trigger (a parent row's menu, the document policy) must not also act.
          event.preventDefault();
          event.stopPropagation();
          void showNativeContextMenu(entries, menuPoint(event), { onOpenChange }).catch((err: unknown) => {
            console.warn("native context menu failed", err);
          });
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
