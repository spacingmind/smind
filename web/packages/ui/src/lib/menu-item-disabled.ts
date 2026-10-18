/*
 * A menu item that is disabled but still reachable: Radix's `disabled` removes
 * the item from roving focus, so a keyboard or screen-reader user never
 * reaches the reason it is unavailable. These props keep the item focusable
 * (aria-disabled, selecting it does nothing and keeps the menu open) and point
 * it at its visible reason via aria-describedby.
 */
export interface ReachableDisabledProps {
  "aria-disabled": true;
  "aria-describedby": string;
  className: string;
}

export function reachableDisabledProps(reasonId: string): ReachableDisabledProps {
  return {
    "aria-disabled": true,
    "aria-describedby": reasonId,
    className: "cursor-not-allowed text-muted-foreground [&_svg]:opacity-60",
  };
}

/** Selecting a reachable-disabled item is a no-op that leaves the menu open. */
export function ignoreSelect(event: Event): void {
  event.preventDefault();
}

/**
 * Class for a menu content whose item opens a dialog: with it the menu
 * disappears on the very frame the dialog starts, instead of fading out
 * underneath the dialog.
 */
export const MENU_CLOSE_INSTANT = "data-[state=closed]:animate-none!";
