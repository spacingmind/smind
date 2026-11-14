import type { ReactNode } from "react";
import { Icons } from "@/lib/icons";
import type { MenuEntry } from "@/lib/menu-model";
import type { Task } from "@/lib/types";
import type { SpaceWithTasks } from "@/lib/workspace-tree";
import type { TreeActionAvailability } from "@/lib/task-tree-actions";

/**
 * The pure builders of the app's context menus (desktop-native-feel DT2 (a)/(b)). Each takes the state the
 * menu depends on and returns the model both renderers draw; none touches the DOM or the daemon. The
 * per-menu table tests (`menu-models.test.tsx`) assert each one's entries, order, separators and enabled
 * state for every state that changes them.
 */

/** What the explorer needs to offer local-only actions (a subset of hooks/use-local-path-actions). */
export interface FileMenuPathActions {
  root: string;
  actions: {
    revealLabel: string;
    editors: { id: string; label: string }[];
    reveal: (absolutePath: string) => void;
    open: (editorId: string, absolutePath: string) => void;
  };
}

export interface FileRowMenuInput {
  path: string;
  /** The file has a diff to reveal. */
  changed: boolean;
  onRevealInDiff: (path: string) => void;
  onOpenToSide?: (path: string) => void;
  onAddToChat?: (path: string) => void;
  /** Present only for the desktop app on its local daemon; the Reveal and Open-in-editor items are absent (not disabled) otherwise. */
  pathActions: FileMenuPathActions | null;
}

export function fileRowMenu(i: FileRowMenuInput): MenuEntry[] {
  const absolutePath = i.pathActions ? `${i.pathActions.root}/${i.path}` : null;
  const entries: MenuEntry[] = [
    { kind: "item", id: "reveal-in-diff", label: "Reveal in diff", icon: <Icons.diff />, enabled: i.changed, testId: "file-menu-reveal-in-diff", onSelect: () => i.onRevealInDiff(i.path) },
    { kind: "item", id: "open-to-side", label: "Open to side", icon: <Icons.openToSide />, enabled: i.onOpenToSide !== undefined, testId: "file-menu-open-to-side", onSelect: () => i.onOpenToSide?.(i.path) },
  ];
  const onAddToChat = i.onAddToChat;
  if (onAddToChat) {
    entries.push({ kind: "item", id: "add-to-chat", label: "Add to chat", icon: <Icons.mention />, testId: "file-menu-add-to-chat", onSelect: () => onAddToChat(i.path) });
  }
  entries.push({
    kind: "item",
    id: "copy-path",
    label: "Copy path",
    icon: <Icons.copy />,
    testId: "file-menu-copy-path",
    onSelect: () => {
      void navigator.clipboard?.writeText(i.path).catch(() => {});
    },
  });
  const pa = i.pathActions;
  if (pa && absolutePath) {
    entries.push({ kind: "separator" });
    entries.push({ kind: "item", id: "reveal", label: pa.actions.revealLabel, icon: <Icons.folderOpen />, testId: "file-menu-reveal", onSelect: () => pa.actions.reveal(absolutePath) });
    for (const editor of pa.actions.editors) {
      entries.push({
        kind: "item",
        id: `open-in-editor:${editor.id}`,
        label: `Open in ${editor.label}`,
        icon: <Icons.externalLink />,
        testId: "file-menu-open-in-editor",
        data: { "editor-id": editor.id },
        onSelect: () => pa.actions.open(editor.id, absolutePath),
      });
    }
  }
  return entries;
}

/** A directory row's menu: only "Add to chat" (a directory reference is valid); without the handler the row has no menu (an empty model). */
export function dirRowMenu(i: { path: string; onAddToChat?: (path: string) => void }): MenuEntry[] {
  const onAddToChat = i.onAddToChat;
  if (!onAddToChat) return [];
  return [{ kind: "item", id: "add-to-chat", label: "Add to chat", icon: <Icons.mention />, testId: "dir-menu-add-to-chat", onSelect: () => onAddToChat(i.path) }];
}

export type TabMenuSplitDirection = "left" | "right" | "up" | "down";

export interface TabMenuInput {
  /** terminal | chat | file | diff | files ... (the TabEntry kind): only the kinds below add items. */
  kind: string;
  /** The file tab's relative path, when it has one. */
  path?: string;
  closable: boolean;
  hasOtherClosableTabs: boolean;
  hasClosableTabsToLeft: boolean;
  hasClosableTabsToRight: boolean;
  /** The kind can be moved to another pane (isMovableKind) AND the layout offers moving (more than one tab or pane). */
  canSplit: boolean;
  onClose: () => void;
  onCloseOthers: () => void;
  onCloseLeft: () => void;
  onCloseRight: () => void;
  onRename: () => void;
  onArchiveChat: () => void;
  onSplit: (direction: TabMenuSplitDirection) => void;
}

/** A workspace tab's menu. Close items are always present and disabled when they cannot run; Rename, Archive and Copy path appear only for the kinds that have them; the split items only when the tab can move. */
export function tabMenu(i: TabMenuInput): MenuEntry[] {
  const hasTail = i.kind === "terminal" || i.kind === "chat" || (i.kind === "file" && i.path !== undefined && i.path !== "");
  const entries: MenuEntry[] = [
    { kind: "item", id: "close", label: "Close", enabled: i.closable, testId: "workspace-tab-menu-close", onSelect: i.onClose },
    { kind: "item", id: "close-others", label: "Close others", enabled: i.hasOtherClosableTabs, testId: "workspace-tab-menu-close-others", onSelect: i.onCloseOthers },
    { kind: "item", id: "close-left", label: "Close to the left", enabled: i.hasClosableTabsToLeft, testId: "workspace-tab-menu-close-left", onSelect: i.onCloseLeft },
    { kind: "item", id: "close-right", label: "Close to the right", enabled: i.hasClosableTabsToRight, testId: "workspace-tab-menu-close-right", onSelect: i.onCloseRight },
  ];
  if (hasTail) entries.push({ kind: "separator" });
  if (i.kind === "terminal" || i.kind === "chat") {
    entries.push({ kind: "item", id: "rename", label: "Rename", icon: <Icons.edit />, testId: "workspace-tab-menu-rename", onSelect: i.onRename });
  }
  if (i.kind === "chat") {
    entries.push({ kind: "item", id: "archive-chat", label: "Archive chat", icon: <Icons.archive />, testId: "workspace-tab-menu-archive-chat", onSelect: i.onArchiveChat });
  }
  const path = i.path;
  if (i.kind === "file" && path) {
    entries.push({
      kind: "item",
      id: "copy-path",
      label: "Copy path",
      icon: <Icons.copy />,
      testId: "workspace-tab-menu-copy-path",
      onSelect: () => {
        void navigator.clipboard?.writeText(path).catch(() => {});
      },
    });
  }
  if (i.canSplit) {
    entries.push({ kind: "separator" });
    const splits: Array<[TabMenuSplitDirection, string, ReactNode]> = [
      ["left", "Split left", <Icons.splitColumns key="l" />],
      ["right", "Split right", <Icons.splitColumns key="r" />],
      ["up", "Split up", <Icons.splitRows key="u" />],
      ["down", "Split down", <Icons.splitRows key="d" />],
    ];
    for (const [direction, label, icon] of splits) {
      entries.push({ kind: "item", id: `split-${direction}`, label, icon, testId: `workspace-tab-menu-split-${direction}`, onSelect: () => i.onSplit(direction) });
    }
  }
  return entries;
}

/**
 * "Move to space" lists every other space in the task's workspace plus
 * "Ungrouped" unless the task is already there (no self-move). It is omitted
 * with no `spaces`/`onMoveTask` pair (SearchResults' flat list spans workspaces
 * and doesn't offer the move) or nowhere to move to.
 */
export function taskRowMenu(
  task: Task,
  o: {
    spaces?: SpaceWithTasks[];
    onMoveTask?: (task: Task, spaceId: number | null) => void;
    isPinned: boolean;
    onTogglePin: (taskId: number) => void;
    onMarkUnread: (taskId: number) => void;
    onArchiveTask: (task: Task) => void;
    /** Opens the New sub-task dialog; absent outside the tree provider (search results). */
    onNewSubTask?: () => void;
    /** PR-H11 tree actions; present only on rows in the tree view whose task has live descendants. */
    tree?: { availability: TreeActionAvailability; onArchiveFinished: () => void; onStopTree: () => void };
  },
): MenuEntry[] {
  const entries: MenuEntry[] = [];
  if (o.onNewSubTask && !task.ArchivedAt) {
    entries.push({
      kind: "item",
      id: "new-subtask",
      testId: "sidebar-task-new-subtask-action",
      label: "New sub-task…",
      icon: <Icons.add />,
      onSelect: o.onNewSubTask,
      opensDialog: true,
    });
  }
  if (o.tree) {
    const { availability, onArchiveFinished, onStopTree } = o.tree;
    entries.push(
      {
        kind: "item",
        id: "archive-finished",
        testId: "sidebar-task-archive-finished-action",
        label: "Archive finished sub-tasks…",
        icon: <Icons.archive />,
        onSelect: onArchiveFinished,
        opensDialog: true,
        enabled: availability.archive.enabled,
        reachable: true,
        note: availability.archive.reason ?? undefined,
      },
      {
        kind: "item",
        id: "stop-tree",
        testId: "sidebar-task-stop-tree-action",
        label: "Stop all in tree…",
        icon: <Icons.stop />,
        onSelect: onStopTree,
        opensDialog: true,
        enabled: availability.stop.enabled,
        reachable: true,
        note: availability.stop.reason ?? undefined,
      },
    );
  }
  if (o.spaces && o.onMoveTask) {
    const targets: { id: number | null; label: string }[] = [];
    if (task.SpaceID !== null) targets.push({ id: null, label: "Ungrouped" });
    for (const space of o.spaces) {
      if (space.ID !== task.SpaceID) targets.push({ id: space.ID, label: space.Title });
    }
    const onMoveTask = o.onMoveTask;
    if (targets.length > 0) {
      entries.push({
        kind: "submenu",
        id: "move",
        label: "Move to space",
        icon: <Icons.space />,
        testId: "sidebar-task-move-action",
        children: targets.map((target) => ({
          kind: "item" as const,
          id: `move:${target.id ?? "ungrouped"}`,
          label: target.label,
          testId: "sidebar-task-move-target",
          data: { "space-id": target.id ?? "ungrouped" },
          onSelect: () => onMoveTask(task, target.id),
        })),
      });
    }
  }
  entries.push(
    {
      kind: "item",
      id: "pin",
      testId: "sidebar-task-pin-action",
      label: o.isPinned ? "Unpin" : "Pin",
      icon: o.isPinned ? <Icons.unpin /> : <Icons.pin />,
      onSelect: () => o.onTogglePin(task.ID),
    },
    {
      kind: "item",
      id: "mark-unread",
      testId: "sidebar-task-mark-unread-action",
      label: "Mark unread",
      icon: <Icons.statusIdle />,
      onSelect: () => o.onMarkUnread(task.ID),
    },
    {
      kind: "item",
      id: "archive",
      testId: "sidebar-task-archive-action",
      label: "Archive task",
      icon: <Icons.archive />,
      onSelect: () => o.onArchiveTask(task),
      opensDialog: true,
    },
  );
  return entries;
}

/** A workspace or space row's actions (the same items its ⋯ menu offers), as a model. Ids are positional (`item-<n>`), so two rows with the same label never collide; `fadeOut` items keep the menu's exit animation, the others close it instantly because they open a dialog. */
export function rowMenu(items: { label: string; icon: ReactNode; onSelect: () => void; fadeOut?: boolean }[]): MenuEntry[] {
  return items.map((item, index) => ({
    kind: "item" as const,
    id: `item-${index}`,
    label: item.label,
    icon: item.icon,
    opensDialog: item.fadeOut !== true,
    onSelect: item.onSelect,
  }));
}
