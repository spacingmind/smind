import type { ReactNode } from "react";

/**
 * The shared two-line layout for DropdownMenuItem / ContextMenuItem: an
 * optional leading icon, the title (with an optional second-line
 * description) and a right-aligned shortcut. With none of the extras it
 * renders children unchanged, so existing menus keep their DOM.
 */
export function MenuItemBody({
  icon,
  description,
  shortcut,
  children,
}: {
  icon?: ReactNode;
  description?: ReactNode;
  shortcut?: string;
  children: ReactNode;
}) {
  if (icon === undefined && description === undefined && shortcut === undefined) {
    return <>{children}</>;
  }
  return (
    <>
      {icon}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">{children}</span>
        {description && (
          <span className="truncate text-ui-sm text-foreground-muted">{description}</span>
        )}
      </span>
      {shortcut && (
        <span
          data-slot="menu-item-shortcut"
          className="ml-auto shrink-0 pl-4 text-ui-sm text-foreground-subtle"
        >
          {shortcut}
        </span>
      )}
    </>
  );
}
