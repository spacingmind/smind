import { useId, useState } from "react";

import { SyncDialog, syncTitle } from "@/components/git/sync-dialog";
import type { SyncMode } from "@/components/git/sync-model";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useTaskGit } from "@/hooks/use-task-git";
import { Icons } from "@/lib/icons";
import { ignoreSelect, reachableDisabledProps } from "@/lib/menu-item-disabled";
import type { TaskGitAction } from "@/lib/types";

export interface SyncEntry {
  mode: SyncMode;
  label: string;
  enabled: boolean;
  /** The daemon's sentence when disabled. */
  reason?: string;
}

/**
 * The two sync actions a task offers right now, from `actions.push` / `actions.updateFromBase` (G7). An action the
 * daemon doesn't report (an older daemon, or no git state yet) is left out rather than shown dead.
 */
export function useSyncEntries(taskId: number): SyncEntry[] {
  const git = useTaskGit(taskId);
  if (!git) return [];
  const base = git.base?.known ? (git.base.ref ?? "") : "";
  const entry = (mode: SyncMode, action: TaskGitAction | undefined): SyncEntry[] =>
    action ? [{ mode, label: syncTitle(mode, git.origin?.published ?? false, base), enabled: action.enabled, reason: action.enabled ? undefined : action.reason }] : [];
  return [...entry("push", git.actions?.push), ...entry("update", git.actions?.updateFromBase)];
}

/** Menu items for the header "…" menu and the narrow Diff menu; the host owns the dialog so it outlives the menu. */
export function SyncMenuItems({ taskId, onOpen }: { taskId: number; onOpen: (mode: SyncMode) => void }) {
  const entries = useSyncEntries(taskId);
  const reasonPrefix = useId();
  return (
    <>
      {entries.map((entry) => {
        const reasonId = `${reasonPrefix}-${entry.mode}`;
        const blocked = !entry.enabled;
        return (
          <DropdownMenuItem
            key={entry.mode}
            data-testid={`sync-menu-${entry.mode}`}
            title={entry.reason}
            {...(blocked ? reachableDisabledProps(reasonId) : {})}
            onSelect={blocked ? ignoreSelect : () => onOpen(entry.mode)}
          >
            {entry.mode === "push" ? <Icons.push aria-hidden /> : <Icons.merge aria-hidden />}
            <span className="flex min-w-0 flex-col">
              <span>{entry.label}…</span>
              {blocked && entry.reason && (
                <span id={reasonId} className="max-w-60 text-ui-xs font-normal whitespace-normal text-muted-foreground" data-testid={`sync-menu-${entry.mode}-reason`}>
                  {entry.reason}
                </span>
              )}
            </span>
          </DropdownMenuItem>
        );
      })}
    </>
  );
}

/**
 * The Diff header's Push / Update buttons, next to `<PrAction>` and in its compact style: a disabled one keeps the
 * daemon's sentence as its tooltip. Below `md` the Diff header uses the "…" menu instead (see `SyncMenuItems`).
 */
export function SyncButtons({ taskId }: { taskId: number }) {
  const entries = useSyncEntries(taskId);
  const [mode, setMode] = useState<SyncMode | null>(null);
  const [lastMode, setLastMode] = useState<SyncMode>("push");
  if (entries.length === 0) return null;
  const open = (m: SyncMode) => {
    setLastMode(m);
    setMode(m);
  };
  return (
    <div className="flex items-center gap-2" data-testid="sync-buttons">
      {entries.map((entry) => (
        <span key={entry.mode} title={entry.reason}>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!entry.enabled}
            onClick={() => open(entry.mode)}
            data-testid={`sync-button-${entry.mode}`}
            aria-label={entry.reason ? `${entry.label}. ${entry.reason}` : undefined}
          >
            {entry.label}
          </Button>
        </span>
      ))}
      <SyncDialog taskId={taskId} mode={lastMode} open={mode !== null} onOpenChange={(next) => !next && setMode(null)} />
    </div>
  );
}
