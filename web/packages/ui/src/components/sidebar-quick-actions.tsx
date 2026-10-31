import type { JSX, ReactNode } from "react";

import { Kbd } from "@/components/ui/kbd";
import { Icons, type IconComponent } from "@/lib/icons";

const ROW_CLASS =
  "flex h-7 w-full max-sm:min-h-9 items-center gap-2 rounded-lg px-2 text-left text-ui-base text-foreground hover:bg-hover focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none";

function QuickActionRow({
  icon: Icon,
  label,
  testId,
  onClick,
  trailing,
}: {
  icon: IconComponent;
  label: string;
  testId: string;
  onClick: () => void;
  trailing?: ReactNode;
}) {
  return (
    <button type="button" data-testid={testId} onClick={onClick} className={ROW_CLASS}>
      <Icon className="size-4 shrink-0 text-foreground-muted" />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {trailing}
    </button>
  );
}

/**
 * The ZCode-style quick actions block at the top of the sidebar: New task,
 * Search (with the palette shortcut) and Automations rows. Presentational --
 * AppSidebar owns the handlers -- and hidden on the icon-collapsed rail.
 */
export function SidebarQuickActions(props: {
  onNewTask: () => void;
  onSearch: () => void;
  onAutomations: () => void;
  searchShortcut?: string | null;
}): JSX.Element {
  return (
    <nav
      aria-label="Quick actions"
      data-testid="sidebar-quick-actions"
      className="flex flex-col gap-0.5 px-2 pb-1 pt-1 group-data-[collapsible=icon]:hidden"
    >
      <QuickActionRow
        icon={Icons.newTask}
        label="New task"
        testId="sidebar-quick-new-task"
        onClick={props.onNewTask}
      />
      <QuickActionRow
        icon={Icons.search}
        label="Search"
        testId="sidebar-quick-search"
        onClick={props.onSearch}
        trailing={props.searchShortcut ? <Kbd keys={props.searchShortcut} /> : undefined}
      />
      <QuickActionRow
        icon={Icons.automation}
        label="Automations"
        testId="sidebar-quick-automations"
        onClick={props.onAutomations}
      />
    </nav>
  );
}
