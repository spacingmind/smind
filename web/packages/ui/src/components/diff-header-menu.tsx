import { SyncMenuItems } from "@/components/git/sync-actions";
import type { SyncMode } from "@/components/git/sync-model";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Icons } from "@/lib/icons";
import type { DiffPrefs } from "@/lib/diff-prefs";

/**
 * The Diff header's actions below `md`: the two view toggles, and Refresh
 * collapsed into one "⋯" menu, so they stop overflowing a phone-
 * width pane (git-github-ux G6). Each choice is a menu item with a check on
 * the active one; the inline header (SegmentedToggle + buttons) is what
 * wider panes keep.
 */
export function DiffHeaderMenu({
  prefs,
  onChangePrefs,
  onRefresh,
  refreshDisabled,
  taskId,
  onSync,
}: {
  prefs: DiffPrefs;
  onChangePrefs: (patch: Partial<DiffPrefs>) => void;
  onRefresh: () => void;
  refreshDisabled: boolean;
  /** With `onSync`: the task's Push / Update items join the menu (the Diff header has no room for their buttons here). */
  taskId?: number;
  onSync?: (mode: SyncMode) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" size="icon-sm" aria-label="Diff actions" data-testid="diff-actions-menu">
          <Icons.more aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" data-testid="diff-actions-menu-content">
        {taskId !== undefined && onSync && (
          <>
            <SyncMenuItems taskId={taskId} onOpen={onSync} />
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuLabel>View</DropdownMenuLabel>
        <ChoiceItem testId="diff-menu-by-file" checked={prefs.view === "by-file"} onSelect={() => onChangePrefs({ view: "by-file" })}>
          By file
        </ChoiceItem>
        <ChoiceItem testId="diff-menu-whole" checked={prefs.view === "whole"} onSelect={() => onChangePrefs({ view: "whole" })}>
          Whole diff
        </ChoiceItem>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Layout</DropdownMenuLabel>
        <ChoiceItem
          testId="diff-menu-unified"
          checked={prefs.format === "line-by-line"}
          onSelect={() => onChangePrefs({ format: "line-by-line" })}
        >
          Unified
        </ChoiceItem>
        <ChoiceItem
          testId="diff-menu-side-by-side"
          checked={prefs.format === "side-by-side"}
          onSelect={() => onChangePrefs({ format: "side-by-side" })}
        >
          Side by side
        </ChoiceItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={refreshDisabled} onSelect={onRefresh} data-testid="diff-menu-refresh">
          <Icons.refresh aria-hidden /> Refresh
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ChoiceItem({
  checked,
  onSelect,
  testId,
  children,
}: {
  checked: boolean;
  onSelect: () => void;
  testId: string;
  children: string;
}) {
  return (
    <DropdownMenuItem onSelect={onSelect} data-testid={testId} role="menuitemradio" aria-checked={checked}>
      <span className="flex size-4 items-center justify-center">{checked && <Icons.check aria-hidden className="size-3.5" />}</span>
      {children}
    </DropdownMenuItem>
  );
}
