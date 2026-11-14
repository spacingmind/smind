import { Icons } from "@/lib/icons";
import type { MenuEntry } from "@/lib/menu-model";

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
    const splits: Array<[TabMenuSplitDirection, string, JSX.Element]> = [
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
