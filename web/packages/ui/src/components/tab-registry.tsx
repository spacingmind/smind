import type { LucideIcon } from "lucide-react";
import { File, FolderTree, GitCompare, MessageSquare, Plus, SquareTerminal } from "lucide-react";

import { FileIcon } from "@/lib/file-icons";
import { useBufferDirty } from "@/lib/dirty-buffers";
import { useTerminalActivity } from "@/lib/terminal-sessions";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/**
 * The tab registry: the vocabulary of tab kinds App.tsx's tab strip is
 * rendered from, per docs/plans/active/tab-registry-side-dock.md. A tab is
 * data ({kind, key, taskId, title, closable}), not hardcoded JSX--the
 * strip maps over a list of these, and adding a new kind later means
 * adding a descriptor here plus a renderer entry in App.tsx, never editing
 * the strip's layout logic.
 *
 * Keys are globally unique *and* task-scoped (docs/decisions/0004-per-task-editor-tabs.md):
 * `${taskId}:task`, `${taskId}:files`, `${taskId}:diff`,
 * `${taskId}:terminal`, `${taskId}:file:${path}` -- so the same file path
 * open in two tasks' tabs never collides, and per-task tab sets fall out
 * of the key structure naturally.
 */
export type TabKind = "task" | "files" | "file" | "diff" | "terminal";

/** The non-file kinds: the ones a task's strip is seeded with and the empty state / "+" menu can (re)open on demand. */
export type BaseTabKind = Exclude<TabKind, "file">;

export const BASE_TAB_KINDS: readonly BaseTabKind[] = ["task", "files", "diff", "terminal"];

/** One entry in a task's tab strip. */
export interface TabEntry {
  kind: TabKind;
  /** Globally unique, task-scoped -- see the file doc comment for the exact formats. */
  key: string;
  taskId: number;
  title: string;
  /** Falls back to TAB_KINDS[kind].closable when absent. */
  closable?: boolean;
  /** The worktree-relative path, for `kind: "file"` only -- what the strip's file-type icon is resolved from (it's also recoverable from `key`, but carrying it avoids every consumer re-parsing the key). */
  path?: string;
}

/**
 * Per-kind defaults: whether tabs of this kind get a close affordance,
 * their strip title when nothing better is known, and the strip icon that
 * identifies the kind at a glance (Item 17). Every kind is closable
 * (web-ui-dogfood-polish Item 3): tabs are user-owned -- closing a task's
 * last tab shows TabsEmptyState rather than being prevented, and
 * defaultTabsForTask only *seeds* a first-visit set.
 */
export interface TabKindDescriptor {
  closable: boolean;
  defaultTitle: string;
  icon: LucideIcon;
}

export const TAB_KINDS: Record<TabKind, TabKindDescriptor> = {
  task: { closable: true, defaultTitle: "Chat", icon: MessageSquare },
  files: { closable: true, defaultTitle: "Files", icon: FolderTree },
  diff: { closable: true, defaultTitle: "Diff", icon: GitCompare },
  terminal: { closable: true, defaultTitle: "Terminal", icon: SquareTerminal },
  file: { closable: true, defaultTitle: "File", icon: File },
};

/** The tab entry for one base kind -- the one constructor behind both the seed set and every "reopen Chat/Files/Diff/Terminal" affordance. */
export function baseTabForKind(taskId: number, kind: BaseTabKind): TabEntry {
  const descriptor = TAB_KINDS[kind];
  return { kind, key: `${taskId}:${kind}`, taskId, title: descriptor.defaultTitle, closable: descriptor.closable };
}

/** The base tabs a task's strip is *seeded* with on first visit (ADR 0004's per-task tab set), in strip order. Nothing here is non-closable anymore (Item 3) -- the user can close all of it and reopen via the empty state or the strip's "+". */
export function defaultTabsForTask(taskId: number): TabEntry[] {
  return BASE_TAB_KINDS.map((kind) => baseTabForKind(taskId, kind));
}

/** The tab key a file path maps to, in one place -- lib/dirty-buffers.ts keys its store by exactly this string, from the editor side, without importing a TabEntry. */
export function fileTabKey(taskId: number, path: string): string {
  return `${taskId}:file:${path}`;
}

/** A closable editor tab for one file path (wire path, "/"-joined relative to the task's worktree root). */
export function fileTab(taskId: number, path: string): TabEntry {
  const segments = path.split("/");
  return {
    kind: "file",
    key: fileTabKey(taskId, path),
    taskId,
    title: segments[segments.length - 1] || path,
    closable: TAB_KINDS.file.closable,
    path,
  };
}

/**
 * An additional terminal tab for a task (Item 20: more than one terminal
 * per task, each its own tab). `index` starts at 2 -- index 1 is the base
 * `${taskId}:terminal` tab every task's seed set already includes, which
 * is closable like every other tab (Item 3).
 */
export function terminalTab(taskId: number, index: number): TabEntry {
  return {
    kind: "terminal",
    key: `${taskId}:terminal:${index}`,
    taskId,
    title: `Terminal ${index}`,
    closable: true,
  };
}

/**
 * The next terminal tab for a task, given its current strip: the lowest
 * unused index from 2 up. Re-uses a number freed by closing a tab rather
 * than counting forever, so a task that's had ten terminals opened and
 * closed doesn't end up with "Terminal 11" next to "Terminal".
 */
export function nextTerminalTab(taskId: number, tabs: TabEntry[]): TabEntry {
  const taken = new Set(tabs.filter((t) => t.kind === "terminal").map((t) => t.key));
  let index = 2;
  while (taken.has(`${taskId}:terminal:${index}`)) index++;
  return terminalTab(taskId, index);
}

/** Extracts a file tab's worktree-relative path from its key. Returns "" for a tab of any other kind. */
export function filePathFromTabKey(key: string): string {
  const marker = ":file:";
  const at = key.indexOf(marker);
  return at === -1 ? "" : key.slice(at + marker.length);
}

/**
 * The strip label for one tab: kind (or file-type) icon, title, and -- for
 * a file tab whose editor has unsaved edits -- a dirty dot
 * (ui-redesign-parity plan, Item 17). Lives here rather than in App.tsx so
 * the strip stays a dumb `map` over TabEntry and the vocabulary of what a
 * tab *looks* like stays next to the vocabulary of what a tab *is*.
 */
export function TabLabel({ entry }: { entry: TabEntry }) {
  const dirty = useBufferDirty(entry.key);
  // Output arrived on a terminal you weren't looking at (Item 20). Only
  // meaningful for terminal tabs; the store is keyed by tab key, so
  // asking for any other kind is a cheap constant false.
  const busy = useTerminalActivity(entry.key);
  const KindIcon = TAB_KINDS[entry.kind].icon;

  return (
    <>
      {entry.kind === "file" && entry.path ? (
        <FileIcon path={entry.path} className="opacity-70" />
      ) : (
        <KindIcon aria-hidden data-testid="tab-icon" data-icon={entry.kind} className="size-3.5 shrink-0 opacity-70" />
      )}
      <span className="min-w-0 truncate">{entry.title}</span>
      {dirty && (
        <span
          data-testid="tab-dirty-marker"
          data-tab-key={entry.key}
          aria-label="unsaved changes"
          className="size-1.5 shrink-0 rounded-full bg-warning"
        />
      )}
      {busy && (
        <span
          data-testid="tab-activity-marker"
          data-tab-key={entry.key}
          aria-label="new output"
          className="size-1.5 shrink-0 rounded-full bg-warning"
        />
      )}
    </>
  );
}

/**
 * What a pane shows once the user closes its last tab (Item 3: tabs are
 * user-owned, so "empty" is a real state, not something to prevent). The
 * buttons are the same four base kinds the "+" menu offers, so the way
 * back in is identical from either surface.
 */
export function TabsEmptyState({ onOpen }: { onOpen: (kind: BaseTabKind) => void }) {
  return (
    <div
      data-testid="tabs-empty-state"
      className="flex h-full flex-col items-center justify-center gap-3 text-ui-base text-muted-foreground"
    >
      <p>No tabs open</p>
      <div className="flex flex-wrap items-center justify-center gap-2">
        {BASE_TAB_KINDS.map((kind) => {
          const Icon = TAB_KINDS[kind].icon;
          return (
            <Button
              key={kind}
              type="button"
              variant="outline"
              size="sm"
              data-testid="tabs-empty-open"
              data-kind={kind}
              onClick={() => onOpen(kind)}
            >
              <Icon aria-hidden className="size-3.5" data-icon="inline-start" />
              Open {TAB_KINDS[kind].defaultTitle}
            </Button>
          );
        })}
      </div>
    </div>
  );
}

/**
 * The tab strip's "+" affordance (Item 3): opens any base tab kind. The
 * parent decides open-vs-activate -- it owns the tab state, so a kind
 * that's already open just comes forward rather than duplicating.
 *
 * `open`/`onOpenChange` are optional: omitted, the menu is the ordinary
 * click-to-open Radix default; passed, the keyboard's `tab.new` action
 * (App.tsx) can pop this exact pane's menu open without a synthetic click.
 */
export function NewTabButton({
  onOpen,
  open,
  onOpenChange,
}: {
  onOpen: (kind: BaseTabKind) => void;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  return (
    <DropdownMenu open={open} onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="Open a tab"
          data-testid="tabs-new-tab"
          className="shrink-0 text-muted-foreground"
        >
          <Plus aria-hidden className="size-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        {BASE_TAB_KINDS.map((kind) => {
          const Icon = TAB_KINDS[kind].icon;
          return (
            <DropdownMenuItem
              key={kind}
              data-testid="tabs-new-tab-item"
              data-kind={kind}
              onClick={() => onOpen(kind)}
            >
              <Icon aria-hidden className="size-3.5 opacity-70" />
              Open {TAB_KINDS[kind].defaultTitle}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
