import type { ComponentType, ReactNode } from "react";

import { ignoreSelect, reachableDisabledProps } from "@/lib/menu-item-disabled";
import { normalizeEntries, type MenuEntry } from "@/lib/menu-model";

/** The Radix wrappers one menu flavour (context or dropdown) renders with; both share every prop used below. */
export interface MenuParts {
  Item: ComponentType<{
    onSelect?: (event: Event) => void;
    children?: ReactNode;
    className?: string;
    disabled?: boolean;
    "aria-disabled"?: boolean;
    "aria-describedby"?: string;
    "data-testid"?: string;
    [dataAttr: `data-${string}`]: string | number | undefined;
  }>;
  Sub: ComponentType<{ children?: ReactNode }>;
  SubTrigger: ComponentType<{ children?: ReactNode; "data-testid"?: string }>;
  SubContent: ComponentType<{ children?: ReactNode }>;
  Separator: ComponentType<{ className?: string }>;
}

/**
 * The in-page renderer of a menu model: what the browser build shows and what the desktop build falls
 * back to nowhere (it pops the native menu instead). An unavailable item with a `note` stays
 * keyboard-reachable (lib/menu-item-disabled.ts); one without is plain-disabled.
 */
export function MenuEntries({
  entries,
  parts,
  onDialogSelect,
}: {
  entries: readonly MenuEntry[];
  parts: MenuParts;
  /** Called when an item that opens a dialog is chosen, before the menu closes. */
  onDialogSelect?: () => void;
}) {
  const { Item, Sub, SubTrigger, SubContent, Separator } = parts;
  return (
    <>
      {normalizeEntries(entries).map((entry, index) => {
        if (entry.kind === "separator") return <Separator key={`sep-${index}`} />;
        if (entry.kind === "submenu") {
          return (
            <Sub key={entry.id}>
              <SubTrigger data-testid={entry.testId}>
                {entry.icon}
                {entry.label}
              </SubTrigger>
              <SubContent>
                <MenuEntries entries={entry.children} parts={parts} onDialogSelect={onDialogSelect} />
              </SubContent>
            </Sub>
          );
        }
        const enabled = entry.enabled !== false;
        const reachable = !enabled && (entry.reachable ?? entry.note !== undefined) && entry.testId !== undefined;
        const data: Record<string, string | number> = {};
        for (const [k, v] of Object.entries(entry.data ?? {})) data[`data-${k}`] = v;
        return (
          <Item
            key={entry.id}
            data-testid={entry.testId}
            {...data}
            {...(reachable ? reachableDisabledProps(`${entry.testId}-reason`) : {})}
            {...(!enabled && !reachable ? { disabled: true } : {})}
            onSelect={
              !enabled
                ? ignoreSelect
                : () => {
                    if (entry.opensDialog) onDialogSelect?.();
                    entry.onSelect();
                  }
            }
          >
            {entry.icon}
            {entry.label}
            {entry.note && (
              <span id={`${entry.testId}-reason`} className="ml-auto pl-2 text-ui-xs text-muted-foreground">
                {entry.note}
              </span>
            )}
          </Item>
        );
      })}
    </>
  );
}
