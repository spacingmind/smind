import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { Columns2, Copy, Pencil } from "lucide-react";
import {
  DndContext,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragMoveEvent,
  type DragOverEvent,
} from "@dnd-kit/core";

import { AppSidebar } from "@/components/app-sidebar";
import { CommandPalette } from "@/components/command-palette";
import { DesktopDaemonBanner } from "@/components/desktop-daemon-banner";
import { DesktopUnreachable } from "@/components/desktop-unreachable";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { QuickOpen } from "@/components/quick-open";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Separator } from "@/components/ui/separator";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup, usePanelRef } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  TAB_KINDS,
  baseTabForKind,
  defaultTabsForTask,
  fileTab,
  filePathFromTabKey,
  NewTabButton,
  nextTerminalTab,
  TabLabel,
  TabsEmptyState,
  type BaseTabKind,
  type TabEntry,
  type TabKind,
} from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { useInitialValue } from "@/hooks/use-initial-value";
import { useIsMobile } from "@/hooks/use-mobile";
import { useTheme } from "@/hooks/use-theme";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useCommands, usePalette } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { useUnreadTasks } from "@/hooks/use-unread-tasks";
import { isMovableKind, useTaskTabs, type PaneId, type SplitDirection, type TabPlacement } from "@/hooks/use-task-tabs";
import { SIDEBAR_ICON_WIDTH, SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidth } from "@/hooks/use-sidebar-width";
import { connectDaemon } from "@/lib/daemon";
import { isDesktop } from "@/lib/platform";
import { watchForReconnect, type ConnectionStatus, type ReconnectHandle } from "@/lib/reconnect";
import { formatRoute, parseRoute, type Route } from "@/lib/route";
import { formatTabTitle } from "@/lib/tab-title";
import { resolveSplitDropPosition, type SplitDropZonePosition } from "@/lib/split-drop-zone";
import { cn } from "@/lib/utils";
import {
  collectAllPanes,
  collectAllTabs,
  DEFAULT_PANE_ID,
  findPaneById,
  MIN_SPLIT_SIZE,
  type SplitNode,
} from "@/lib/split-tree";
import { findAdjacentPane, type PaneDirection } from "@/lib/split-navigation";
import type { ThemePreference } from "@/lib/theme";
import type { Task, TaskFilesResult, Workspace } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** `useDraggable`'s `data` for a tab strip entry (Item 8) -- carries what `onDragEnd` needs to decide move-vs-split without re-deriving it from the tree. */
interface DraggedTabData {
  tabKey: string;
  sourcePaneId: string;
  kind: TabKind;
}

/** Maps a drop's edge zone (`SplitDropZonePosition`, minus `"center"`) to `use-task-tabs.ts`'s own up/down `SplitDirection` naming. */
const DROP_POSITION_TO_SPLIT_DIRECTION: Record<Exclude<SplitDropZonePosition, "center">, SplitDirection> = {
  left: "left",
  right: "right",
  top: "up",
  bottom: "down",
};

const STATUS_LABEL: Record<ConnectionStatus, string> = {
  connecting: "Connecting…",
  connected: "Connected to daemon",
  reconnecting: "Reconnecting to daemon…",
  disconnected: "Disconnected",
};

/**
 * The app shell: a collapsible sidebar with live workspace/task data next
 * to a resizable main content area that shows the selected task once a
 * task is clicked in the sidebar, client-side only (no navigation).
 *
 * The selected task's pane is a registry-driven tab strip (ADR 0004): the
 * task's TabEntry list, rendered from data with a kind→renderer lookup,
 * not hardcoded JSX per tab. Each task keeps its own tab set (per-task
 * scoping); `key={task.ID}` on the Tabs root gives every task a fresh
 * mount, the same detach-on-switch contract every pane already
 * implements. Radix's default tab behavior (unmount inactive content) is
 * intentional here, not just accepted: switching away from the Terminal
 * tab unmounts TerminalPane, which detaches its live subscription
 * without closing the session -- exactly the same "switching away only
 * detaches" contract every one of these panes already implements on task
 * switch/unmount.
 */
export function App({
  connect = connectDaemon,
}: {
  /** Overridable for tests -- see TerminalPane's createTerminal for the same pattern. Defaults to the real fetch-token-then-dial flow, used both for the initial connect and (via watchForReconnect) every subsequent reconnect attempt. */
  connect?: () => Promise<WsClient>;
} = {}) {
  // KeyboardProvider wraps the shell rather than being mounted alongside
  // it in main.tsx: `useActionHandler` is how every surface inside claims
  // an action, so the provider has to be an *ancestor* of all of them --
  // including AppShell itself, which claims the shell-level actions.
  return (
    <KeyboardProvider>
      <PaletteProvider>
        <AppShell connect={connect} />
      </PaletteProvider>
    </KeyboardProvider>
  );
}

function AppShell({ connect }: { connect: () => Promise<WsClient> }) {
  // Item 21: below the 768px breakpoint (Tailwind's own `md`, and the
  // number `hooks/use-mobile.ts` already used for the shadcn Sidebar's
  // built-in mobile Sheet), the sidebar-vs-content split and the side
  // dock (Item 6) both stop being resizable panels -- see the compact
  // branch below.
  const isMobile = useIsMobile();
  const [client, setClient] = useState<WsClient | null>(null);
  const [connectError, setConnectError] = useState<string | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>("connecting");
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  // Which settings section a fresh mount of SettingsScreen should land on --
  // `shortcuts.help` deep-links to "shortcuts" (AC5: the old Shift+? dialog
  // is now a Settings section, not a separate surface); every other path
  // into Settings clears this back to null (the screen's own default).
  const [settingsInitialSectionId, setSettingsInitialSectionId] = useState<string | null>(null);
  // Every task across the tree, handed up by AppSidebar (the one component
  // that already fetches it) so the shell can walk it for task.prev/next.
  const [allTasks, setAllTasks] = useState<Task[]>([]);
  const [allWorkspaces, setAllWorkspaces] = useState<Workspace[]>([]);
  // Set once AppSidebar's first workspace/space/task fetch resolves --
  // the signal that "the tree came back empty/without this task" means
  // the task is really gone, not merely not-loaded-yet. Without it, a
  // deep link to an archived task would wait on `pendingRoute` forever
  // instead of degrading to the empty state.
  const [treeLoaded, setTreeLoaded] = useState(false);
  // The route this mount started at, consumed (and cleared) once its
  // target task is found in `allTasks` or the tree finishes loading
  // without it -- see the restore effect below.
  const [pendingRoute, setPendingRoute] = useState<Route | null>(() =>
    typeof window === "undefined" ? null : parseRoute(window.location.hash),
  );
  const [quickOpenOpen, setQuickOpenOpen] = useState(false);
  // Which full-pane surface the main area shows: the task pane or the
  // settings screen (dogfood Item 4). Plain view state, not a route --
  // the plan's "no router library introduction" decision; settings is not
  // deep-linkable and does not disturb the hash the task routing below
  // owns.
  const [activeView, setActiveView] = useState<"workspace" | "settings">("workspace");

  const {
    tabsByTask,
    ensureTask,
    openTab,
    closeTab,
    activate,
    moveTab,
    splitTab,
    resizeGroup,
    focusPane,
    closePane,
    splitPaneEmpty,
    moveTabToNextPane,
    closeOtherTabs,
    closeTabsToLeft,
    closeTabsToRight,
    renameTab,
  } = useTaskTabs();
  const events = useDaemonEvents(client);
  const { attention, runStatus } = useTaskAttention(client, selectedTask?.ID ?? null, events);
  // null until the tree's first successful load (treeLoaded), so an
  // archived/deleted task can be pruned from `unread` without an empty
  // *initial* task list wiping out a persisted unread set before the real
  // fetch even lands -- see useUnreadTasks' own doc comment.
  const liveTaskIds = useMemo(
    () => (treeLoaded ? new Set(allTasks.map((t) => t.ID)) : null),
    [treeLoaded, allTasks],
  );
  const { unread, markUnread } = useUnreadTasks(attention, selectedTask?.ID ?? null, liveTaskIds);

  // AC2: the tab title mirrors the sidebar's own unread count live -- a
  // plain effect, not a ref, since document.title has no React-owned
  // counterpart to diff against.
  useEffect(() => {
    if (typeof document !== "undefined") document.title = formatTabTitle("smind", unread.size);
  }, [unread]);

  // The sidebar's user-resized width (px), persisted across reloads -- see
  // the plan's Item 6. react-resizable-panels' Panel API takes numeric
  // defaultSize/minSize/maxSize as *pixels* directly (only unitless
  // strings are treated as percentages), so these pixel values pass
  // straight through with no percentage conversion needed.
  const [sidebarWidth, setSidebarWidth] = useSidebarWidth();
  // `sidebarWidth` updates on every drag frame (via `onResize` below) so
  // the live value can drive the sidebar's CSS var and get persisted --
  // but react-resizable-panels' `defaultSize` is documented as the
  // panel's *initial* size only, and re-registers the panel (fighting the
  // in-progress drag) whenever that prop's value changes. Freezing it
  // here breaks that feedback loop.
  const initialSidebarWidth = useInitialValue(sidebarWidth, "sidebar");

  // Icon-collapsed dead space: shadcn's Sidebar shrinks its own *visual*
  // content to the icon rail via CSS (`--sidebar-width-icon`), but that's
  // independent of the ResizablePanel wrapping it -- toggling collapse
  // used to leave the panel at its last dragged/default width, an empty
  // gap between the icon rail and the resize handle. Lifting `open` here
  // (as SidebarProvider's controlled prop, rather than its own internal
  // state) lets this same effect drive the panel's actual width via its
  // imperative handle whenever collapse state changes, in either
  // direction (Ctrl+B, the sidebar trigger button, or a future caller).
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const sidebarPanelRef = usePanelRef();
  useEffect(() => {
    if (sidebarOpen) {
      sidebarPanelRef.current?.expand();
    } else {
      sidebarPanelRef.current?.collapse();
    }
  }, [sidebarOpen, sidebarPanelRef]);

  useEffect(() => {
    let cancelled = false;
    let reconnectHandle: ReconnectHandle | null = null;

    connect()
      .then((c) => {
        if (cancelled) {
          c.close();
          return;
        }
        setConnectionStatus("connected");
        setClient(c);
        // Arms the reconnect loop only after a first successful connect --
        // an initial-connect failure keeps the pre-existing connectError
        // behavior below, unchanged, rather than retrying silently.
        reconnectHandle = watchForReconnect(c, {
          connect,
          onStatusChange: setConnectionStatus,
          onClient: (newClient) => {
            if (cancelled) {
              newClient.close();
              return;
            }
            // A genuinely new WsClient instance, not the same one mutated
            // in place -- this is what makes every hook keyed on the
            // `client` reference (useWorkspaceTree, useRunTimeline, ...)
            // re-run and resync itself for free. See the plan's Decisions.
            setClient(newClient);
          },
        });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setConnectionStatus("disconnected");
          setConnectError(err instanceof Error ? err.message : String(err));
        }
      });

    return () => {
      cancelled = true;
      reconnectHandle?.close();
    };
  }, [connect]);

  const selectTask = useCallback(
    (task: Task) => {
      setSelectedTask(task);
      ensureTask(task.ID);
    },
    [ensureTask],
  );

  function openFileTab(path: string) {
    if (!selectedTask) return;
    // Implicit open (file-tree click, a tool-call's click-through): prefer
    // the side pane if the task already has one, but never yank a tab
    // already placed -- openTab's own "prefer" placement is exactly this
    // rule.
    openTab(selectedTask.ID, fileTab(selectedTask.ID, path), "prefer");
  }

  /**
   * The explorer's "Open to side" row action (Item 6/17): unlike
   * `openFileTab`'s implicit "prefer", this always lands in the side pane,
   * creating one if the task doesn't have one yet -- the explicit
   * placement Item 6's own acceptance criteria distinguish from the
   * implicit case.
   */
  function openFileTabToSide(path: string) {
    if (!selectedTask) return;
    openTab(selectedTask.ID, fileTab(selectedTask.ID, path), "side");
  }

  /**
   * The explorer's "Reveal in diff" row action (Item 17). The *payload*
   * travels through lib/diff-reveal.ts's latch, which the diff pane reads
   * on mount -- the shell's only job is to bring that tab forward.
   */
  function revealInDiff() {
    if (!selectedTask) return;
    activate(selectedTask.ID, `${selectedTask.ID}:diff`);
  }

  /**
   * Opens (or activates) one of the base tab kinds -- Item 3's "+"
   * menu and the pane empty state both funnel through here. openTab
   * itself already activates an open tab instead of duplicating it, and
   * `prefer` keeps the existing "implicit opens land where a tab already
   * is, else primary" rule; the strip's own "+" pins to its pane.
   */
  function openBaseTab(kind: BaseTabKind, placement?: TabPlacement) {
    if (!selectedTask) return;
    openTab(selectedTask.ID, baseTabForKind(selectedTask.ID, kind), placement ?? "prefer");
  }

  /** Opens another terminal tab for the selected task (Item 20). The pane picks its own session -- see lib/terminal-sessions.ts. */
  function openTerminalTab() {
    if (!selectedTask) return;
    const state = tabsByTask.get(selectedTask.ID);
    const tabs = state ? collectAllTabs(state.root) : [];
    openTab(selectedTask.ID, nextTerminalTab(selectedTask.ID, tabs));
  }

  /** Splits paneId's tab in `direction` -- the tab-strip's own affordance (Item 3), targeting whichever pane it was clicked from. */
  function splitPaneTab(paneId: string, key: string, direction: SplitDirection) {
    if (!selectedTask) return;
    splitTab(selectedTask.ID, key, paneId, direction);
  }

  /** Persists a resize handle drag's new sizes for one group in the selected task's split tree. */
  function resizePaneGroup(groupId: string, sizes: number[]) {
    if (!selectedTask) return;
    resizeGroup(selectedTask.ID, groupId, sizes);
  }

  // --- Tab context menu (Item 6: close others/left/right, rename, copy path) ---

  function closeOtherTabsForTask(key: string) {
    if (!selectedTask) return;
    closeOtherTabs(selectedTask.ID, key);
  }

  function closeTabsToLeftForTask(key: string) {
    if (!selectedTask) return;
    closeTabsToLeft(selectedTask.ID, key);
  }

  function closeTabsToRightForTask(key: string) {
    if (!selectedTask) return;
    closeTabsToRight(selectedTask.ID, key);
  }

  function renameTabForTask(key: string, title: string) {
    if (!selectedTask) return;
    renameTab(selectedTask.ID, key, title);
  }

  // --- Drag-to-split (Item 8) -------------------------------------------
  //
  // Every tab in a pane's strip is draggable (PaneTabStrip's TabsTrigger,
  // via useDraggable) and every pane is droppable (also PaneTabStrip, via
  // useDroppable) -- see those below. The live drop target/zone is lifted
  // here rather than kept local to any one pane, since it's a property of
  // the whole split tree during a drag, not of the pane the drag started
  // in.
  const [dragOverPaneId, setDragOverPaneId] = useState<string | null>(null);
  const [dropPosition, setDropPosition] = useState<SplitDropZonePosition | null>(null);
  const [isDragActive, setIsDragActive] = useState(false);

  /** Which pane's "+" menu the `tab.new` keyboard action popped open, if any -- see PaneTabStrip's NewTabButton. */
  const [openNewTabPaneId, setOpenNewTabPaneId] = useState<string | null>(null);

  const dndSensors = useSensors(
    useSensor(PointerSensor, {
      // 8px so a plain click-to-activate-tab isn't swallowed as a drag
      // start -- same threshold paseo's own split-container uses.
      activationConstraint: { distance: 8 },
    }),
  );

  const handleDragStart = useCallback(() => {
    setIsDragActive(true);
  }, []);

  const handleDragCancel = useCallback(() => {
    setIsDragActive(false);
    setDragOverPaneId(null);
    setDropPosition(null);
  }, []);

  /**
   * Recomputes the live drop target + zone from the dragged tab's
   * translated rect and whichever pane it's currently over -- shared by
   * onDragMove/onDragOver (matching paseo's own single-handler wiring).
   * A translated center that's currently outside the hovered pane's
   * bounds (can happen mid-frame) clears the preview instead of resolving
   * a bogus position, mirroring paseo's own NaN/out-of-bounds guard.
   */
  const handleDragOver = useCallback((event: DragMoveEvent | DragOverEvent) => {
    const overPaneId = event.over ? String(event.over.id) : null;
    const overRect = event.over?.rect;
    const translatedRect = event.active.rect.current.translated;
    if (!overPaneId || !overRect || !translatedRect) {
      setDragOverPaneId(null);
      setDropPosition(null);
      return;
    }
    const centerX = translatedRect.left + translatedRect.width / 2;
    const centerY = translatedRect.top + translatedRect.height / 2;
    const relativeX = centerX - overRect.left;
    const relativeY = centerY - overRect.top;
    if (relativeX < 0 || relativeX > overRect.width || relativeY < 0 || relativeY > overRect.height) {
      setDragOverPaneId(null);
      setDropPosition(null);
      return;
    }
    setDragOverPaneId(overPaneId);
    setDropPosition(
      resolveSplitDropPosition({ width: overRect.width, height: overRect.height, x: relativeX, y: relativeY }),
    );
  }, []);

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const data = event.active.data.current as DraggedTabData | undefined;
      const targetPaneId = dragOverPaneId;
      const position = dropPosition;
      setIsDragActive(false);
      setDragOverPaneId(null);
      setDropPosition(null);
      if (!data || !targetPaneId || !position || !selectedTask) return;
      if (position === "center") {
        moveTab(selectedTask.ID, data.tabKey, targetPaneId);
        return;
      }
      // Only *splitting* (an edge drop) is gated to movable kinds -- a
      // center drop of any tab (including Chat/Files) still just moves it,
      // matching what clicking it there would do.
      if (!isMovableKind(data.kind)) return;
      splitTab(selectedTask.ID, data.tabKey, targetPaneId, DROP_POSITION_TO_SPLIT_DIRECTION[position]);
    },
    [dragOverPaneId, dropPosition, selectedTask, moveTab, splitTab],
  );

  const taskState = selectedTask ? tabsByTask.get(selectedTask.ID) : undefined;
  const focusedPane = taskState ? findPaneById(taskState.root, taskState.focusedPaneId) : null;
  const paneCount = taskState ? collectAllPanes(taskState.root).length : 0;

  // --- Routing (Item 3) ------------------------------------------------
  //
  // Hash routing: the URL is a *mirror* of selection state, not its
  // source. Selecting a task or switching tabs writes the hash; a
  // hashchange (back/forward, a hand-typed URL, or our own write) feeds
  // back through `pendingRoute`, which the restore effect below resolves
  // against whatever the tree currently knows. Both directions go through
  // the same path, so there's exactly one place that turns a route into a
  // selection.

  useEffect(() => {
    function onHashChange() {
      setPendingRoute(parseRoute(window.location.hash));
    }
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  useEffect(() => {
    if (!pendingRoute) return;
    const match = allTasks.find((t) => t.ID === pendingRoute.taskId);
    if (match) {
      selectTask(match);
      if (pendingRoute.tab.kind === "file") {
        openTab(match.ID, fileTab(match.ID, pendingRoute.tab.path));
      } else if (pendingRoute.tab.kind !== "task") {
        // Not just activate(): only Chat is seeded on first visit now, so
        // a deep link to e.g. .../diff must be able to open that tab, not
        // just activate an entry that may not exist yet. openTab already
        // no-ops to a plain activate when the tab is already open.
        openTab(match.ID, baseTabForKind(match.ID, pendingRoute.tab.kind));
      }
      setPendingRoute(null);
    } else if (treeLoaded) {
      // The tree has answered and this task isn't in it (archived,
      // deleted, or never existed) -- degrade to the empty state rather
      // than waiting on a task that will never arrive.
      setPendingRoute(null);
    }
  }, [pendingRoute, allTasks, treeLoaded, selectTask, openTab, activate]);

  useEffect(() => {
    if (!selectedTask) return;
    // With an arbitrary tree of panes there's no single "primary" story to
    // lean on any more -- whichever pane the user last focused is the only
    // rule that generalizes (see the plan's Decisions).
    const activeEntry = focusedPane?.tabs.find((t) => t.key === focusedPane.activeKey);
    const tab: Route["tab"] =
      activeEntry?.kind === "file"
        ? { kind: "file", path: filePathFromTabKey(activeEntry.key) }
        : { kind: (activeEntry?.kind ?? "task") as Exclude<TabKind, "file"> };
    const nextHash = formatRoute({ workspaceId: selectedTask.WorkspaceID, taskId: selectedTask.ID, tab });
    // Comparing against the live hash (not a ref of "what we last wrote")
    // is what keeps this idempotent under the hashchange listener above:
    // a route restore that lands on the same selection the URL already
    // named writes nothing, so there's no write -> hashchange -> write
    // loop even though both effects run on every relevant state change.
    if (window.location.hash !== nextHash) {
      window.location.hash = nextHash;
    }
  }, [selectedTask, focusedPane]);

  // --- Keyboard actions the shell itself performs ---------------------
  //
  // Everything below is claimed through `useActionHandler`, never through
  // a key listener: the binding table (`keyboard/shortcuts.ts`) decides
  // *which keys* reach these, and the command palette (Item 5) can invoke
  // the same handlers with no key involved. Actions that belong to a pane
  // rather than the shell -- `composer.focus`, `run.interrupt` -- are
  // deliberately absent here; the composer claims them itself.

  useActionHandler("shortcuts.help", () => {
    setSettingsInitialSectionId("shortcuts");
    setActiveView("settings");
  });

  const { open: paletteOpen, setOpen: setPaletteOpen } = usePalette();
  useActionHandler("palette.open", () => setPaletteOpen(!paletteOpen));

  const { preference, setPreference } = useTheme();
  useActionHandler("theme.cycle", () => {
    const order: ThemePreference[] = ["light", "dark", "system"];
    const next = order[(order.indexOf(preference) + 1) % order.length]!;
    setPreference(next);
  });

  // Both bindings target the *focused* pane -- the last one the user
  // interacted with (click, tab switch, or a pane-focus action below), not
  // always the default one. See the plan's Decisions ("Tab actions target
  // the focused pane, not just the default one").
  useActionHandler(
    "tab.close",
    () => {
      if (!selectedTask || !focusedPane?.activeKey) return;
      const active = focusedPane.tabs.find((t) => t.key === focusedPane.activeKey);
      // Every tab is closable (Item 3), but the shortcut still mirrors
      // the strip: no active tab (everything closed) means nothing to do.
      if (!active?.closable) return;
      closeTab(selectedTask.ID, active.key);
    },
    { enabled: Boolean(selectedTask && focusedPane?.activeKey) },
  );

  useActionHandler(
    "tab.jump",
    (payload) => {
      if (!selectedTask || !focusedPane || payload === null) return;
      const entry = focusedPane.tabs[payload.digit - 1];
      if (entry) activate(selectedTask.ID, entry.key);
    },
    { enabled: Boolean(selectedTask && focusedPane) },
  );

  /** Steps the focused pane's active tab by delta, wrapping -- shared by tab.next/tab.prev. */
  const stepFocusedTab = useCallback(
    (delta: 1 | -1) => {
      if (!selectedTask || !focusedPane || focusedPane.tabs.length === 0) return;
      const current = focusedPane.tabs.findIndex((t) => t.key === focusedPane.activeKey);
      const next = current === -1 ? 0 : (current + delta + focusedPane.tabs.length) % focusedPane.tabs.length;
      const entry = focusedPane.tabs[next];
      if (entry) activate(selectedTask.ID, entry.key);
    },
    [selectedTask, focusedPane, activate],
  );
  useActionHandler("tab.next", () => stepFocusedTab(1), {
    enabled: Boolean(selectedTask && focusedPane && focusedPane.tabs.length > 1),
  });
  useActionHandler("tab.prev", () => stepFocusedTab(-1), {
    enabled: Boolean(selectedTask && focusedPane && focusedPane.tabs.length > 1),
  });

  useActionHandler(
    "tab.new",
    () => {
      if (!focusedPane) return;
      setOpenNewTabPaneId(focusedPane.id);
    },
    { enabled: Boolean(focusedPane) },
  );

  useActionHandler(
    "pane.split.right",
    () => {
      if (!selectedTask || !focusedPane) return;
      splitPaneEmpty(selectedTask.ID, focusedPane.id, "right");
    },
    { enabled: Boolean(selectedTask && focusedPane) },
  );
  useActionHandler(
    "pane.split.down",
    () => {
      if (!selectedTask || !focusedPane) return;
      splitPaneEmpty(selectedTask.ID, focusedPane.id, "down");
    },
    { enabled: Boolean(selectedTask && focusedPane) },
  );

  useActionHandler(
    "pane.close",
    () => {
      if (!selectedTask || !focusedPane) return;
      closePane(selectedTask.ID, focusedPane.id);
    },
    { enabled: Boolean(selectedTask && focusedPane && paneCount > 1) },
  );

  useActionHandler(
    "pane.move-tab.next",
    () => {
      if (!selectedTask || !focusedPane?.activeKey) return;
      moveTabToNextPane(selectedTask.ID, focusedPane.activeKey, focusedPane.id);
    },
    { enabled: Boolean(selectedTask && focusedPane?.activeKey && paneCount > 1) },
  );

  const focusAdjacentPane = useCallback(
    (direction: PaneDirection) => {
      if (!selectedTask || !taskState || !focusedPane) return;
      const adjacent = findAdjacentPane(taskState.root, focusedPane.id, direction);
      if (adjacent) focusPane(selectedTask.ID, adjacent);
    },
    [selectedTask, taskState, focusedPane, focusPane],
  );
  // `enabled` here only gates "is there a pane tree to navigate at all" --
  // whether the *default* combo is blocked in a text field (and lifted
  // once the user rebinds it) is `editableWhenRebound` in the binding
  // table (`keyboard/shortcuts.ts`), not something this handler decides.
  useActionHandler("pane.focus.left", () => focusAdjacentPane("left"), { enabled: paneCount > 1 });
  useActionHandler("pane.focus.right", () => focusAdjacentPane("right"), { enabled: paneCount > 1 });
  useActionHandler("pane.focus.up", () => focusAdjacentPane("up"), { enabled: paneCount > 1 });
  useActionHandler("pane.focus.down", () => focusAdjacentPane("down"), { enabled: paneCount > 1 });

  useActionHandler("settings.open", () => {
    setSettingsInitialSectionId(null);
    setActiveView("settings");
  });

  useActionHandler(
    "sidebar.task-jump",
    (payload) => {
      if (payload === null) return;
      const task = allTasks[payload.digit - 1];
      if (task) selectTask(task);
    },
    { enabled: allTasks.length > 0 },
  );

  const stepTask = useCallback(
    (delta: 1 | -1) => {
      if (allTasks.length === 0) return;
      const current = allTasks.findIndex((t) => t.ID === selectedTask?.ID);
      // Wraps, like Paseo's own workspace-prev/next: at either end the
      // next press lands on the other end rather than doing nothing, so
      // holding one direction cycles the whole list.
      const next = current === -1 ? 0 : (current + delta + allTasks.length) % allTasks.length;
      selectTask(allTasks[next]!);
    },
    [allTasks, selectedTask, selectTask],
  );
  useActionHandler("task.prev", () => stepTask(-1), { enabled: allTasks.length > 0 });
  useActionHandler("task.next", () => stepTask(1), { enabled: allTasks.length > 0 });
  useActionHandler("quick-open.open", () => setQuickOpenOpen(true), { enabled: selectedTask !== null });

  // Item 21: shared across both the compact and desktop branches below so
  // the two layouts don't hand-duplicate AppSidebar/header/content JSX --
  // only one of them is ever mounted at a time (isMobile flips which),
  // which is the normal "element as a variable" React pattern, not two
  // live instances.
  const sidebarElement = (
    <AppSidebar
      client={client}
      selectedTaskId={selectedTask?.ID ?? null}
      onSelectTask={selectTask}
      attention={attention}
      runStatus={runStatus}
      unread={unread}
      onMarkUnread={markUnread}
      events={events}
      onTasksChange={setAllTasks}
      onWorkspacesChange={(workspaces) => {
        setAllWorkspaces(workspaces);
        setTreeLoaded(true);
      }}
      onOpenSettings={() => {
        setSettingsInitialSectionId(null);
        setActiveView("settings");
      }}
    />
  );

  const headerElement = (
    <>
      <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
        <SidebarTrigger />
        <Separator orientation="vertical" className="h-4" />
        <span className="text-ui-base text-muted-foreground" data-testid="app-connection-status">
          {connectError ? `Disconnected: ${connectError}` : STATUS_LABEL[connectionStatus]}
        </span>
      </header>
      {isDesktop && <DesktopDaemonBanner />}
    </>
  );

  const emptyStateElement = (
    <div data-testid="app-empty-state" className="flex h-full items-center justify-center text-ui-base text-muted-foreground">
      Select a task to get started.
    </div>
  );

  // Item 21: on compact, the split tree gracefully doesn't apply -- every
  // pane merges into one strip (no "split" affordance, via PaneTabStrip's
  // showMoveAffordance) rather than a layout with nowhere to put a second
  // (or third) pane on a phone. `activate`/`closeTab` are already
  // pane-agnostic (useTaskTabs.ts finds a key's pane by lookup), so every
  // merged tab keeps working through the same handlers unmodified. The
  // currently-focused pane's activeKey wins the merged strip's initial
  // focus -- a non-default pane only exists because something was
  // deliberately opened or split into it, so that's more likely to be
  // what the user was just looking at than whichever base tab the
  // default pane was last on.
  const compactTabs = taskState ? collectAllTabs(taskState.root) : [];
  const compactActiveKey = focusedPane?.activeKey ?? null;
  const compactPrimaryStrip = selectedTask && taskState && (
    <PaneTabStrip
      paneId={DEFAULT_PANE_ID}
      tabs={compactTabs}
      activeKey={compactActiveKey}
      paneCount={paneCount}
      task={selectedTask}
      client={client}
      connectionStatus={connectionStatus}
      events={events}
      onOpenFile={openFileTab}
      onOpenFileToSide={openFileTabToSide}
      onActivate={(key) => activate(selectedTask.ID, key)}
      onClose={(key) => closeTab(selectedTask.ID, key)}
      onCloseOthers={closeOtherTabsForTask}
      onCloseLeft={closeTabsToLeftForTask}
      onCloseRight={closeTabsToRightForTask}
      onRenameTab={renameTabForTask}
      onSplit={() => {}}
      onRevealInDiff={revealInDiff}
      onNewTerminal={openTerminalTab}
      onOpenBase={openBaseTab}
      showMoveAffordance={false}
      newTabMenuOpen={openNewTabPaneId === DEFAULT_PANE_ID}
      onNewTabMenuOpenChange={(open) => setOpenNewTabPaneId(open ? DEFAULT_PANE_ID : null)}
    />
  );

  // Dogfood Item 4: the settings screen replaces the task pane wholesale
  // (same surface) while active. Unmounting the task pane on the way in
  // is intentional and matches the detach-on-switch contract every pane
  // already implements for tab/task switches -- terminal sessions detach,
  // not close, and reattach when the view returns.
  const mainContentElement = (
    <div className="flex-1 min-h-0">
      {activeView === "settings" ? (
        <SettingsScreen
          client={client}
          onNavigateBack={() => setActiveView("workspace")}
          initialSectionId={settingsInitialSectionId ?? undefined}
        />
      ) : isDesktop && connectionStatus === "disconnected" ? (
        // AC6: replaces offline.html's job for the bundled flow -- shown
        // only for the "initial connect never succeeded" case (see
        // ConnectionStatus's own comment in reconnect.ts: an established
        // connection dropping goes through the automatic reconnect loop
        // and its own header status text instead, never this).
        <DesktopUnreachable
          onSwitchConnection={() => {
            setSettingsInitialSectionId("connections");
            setActiveView("settings");
          }}
        />
      ) : selectedTask && taskState ? (
        isMobile ? (
          compactPrimaryStrip
        ) : (
          // The split tree (pane-split-tree plan): an arbitrary number of
          // panes, each its own Radix Tabs root. Splitting or moving a
          // tab between panes is a plain data move in useTaskTabs -- the
          // pane component underneath *does* fully unmount from one root
          // and mount in another, which is fine because every such
          // component already tolerates ADR 0004's "switching tabs
          // unmounts inactive content" and reattaches to its server-side
          // session rather than recreating it (see use-task-tabs.ts's doc
          // comment).
          //
          // DndContext (Item 8) only wraps this desktop/split branch, not
          // compactPrimaryStrip above -- compact mode has nowhere to drop
          // a split into, same reasoning as showMoveAffordance={false}
          // already gating the Split menu off there.
          <DndContext
            sensors={dndSensors}
            onDragStart={handleDragStart}
            onDragMove={handleDragOver}
            onDragOver={handleDragOver}
            onDragEnd={handleDragEnd}
            onDragCancel={handleDragCancel}
          >
            <SplitTreeView
              node={taskState.root}
              paneCount={paneCount}
              task={selectedTask}
              client={client}
              connectionStatus={connectionStatus}
              events={events}
              onOpenFile={openFileTab}
              onOpenFileToSide={openFileTabToSide}
              onActivate={(key) => activate(selectedTask.ID, key)}
              onClose={(key) => closeTab(selectedTask.ID, key)}
              onCloseOthers={closeOtherTabsForTask}
              onCloseLeft={closeTabsToLeftForTask}
              onCloseRight={closeTabsToRightForTask}
              onRenameTab={renameTabForTask}
              onSplitTab={splitPaneTab}
              onResizeGroup={resizePaneGroup}
              onRevealInDiff={revealInDiff}
              onNewTerminal={openTerminalTab}
              onOpenBase={openBaseTab}
              dragOverPaneId={dragOverPaneId}
              dropPosition={dropPosition}
              isDragActive={isDragActive}
              focusedPaneId={taskState.focusedPaneId}
              onFocusPane={(paneId) => focusPane(selectedTask.ID, paneId)}
              openNewTabPaneId={openNewTabPaneId}
              onOpenNewTabPaneIdChange={setOpenNewTabPaneId}
            />
          </DndContext>
        )
      ) : (
        emptyStateElement
      )}
    </div>
  );

  return (
    // SidebarProvider's own wrapper only sets min-h-svh (a floor, not a
    // definite height), which used to be fine when its child just flowed
    // in document order -- ResizablePanelGroup's inner Panels need a
    // *definite* ancestor height to resolve their percentage-based
    // stretch, so an explicit h-svh here (additive with min-h-svh, not
    // conflicting -- different CSS properties) is what actually gives the
    // resize handle its full-height drag area instead of collapsing to
    // the height of the header row.
    <SidebarProvider
      className="h-svh"
      style={{ "--sidebar-width": `${sidebarWidth}px` } as CSSProperties}
      open={sidebarOpen}
      onOpenChange={setSidebarOpen}
    >
      {/*
       * `sidebar.toggle` is claimed by a child of SidebarProvider rather
       * than by AppShell, because `useSidebar()` throws outside it. A
       * zero-render component is the cheapest way to be inside a provider
       * its own parent mounts.
       */}
      <SidebarToggleAction />
      <CommandPalette />
      <ShellCommands
        client={client}
        tasks={allTasks}
        workspaces={allWorkspaces}
        selectedTask={selectedTask}
        tabs={taskState ? collectAllTabs(taskState.root) : null}
        onSelectTask={selectTask}
        onOpenTab={openTab}
        onActivateTab={activate}
      />
      {isMobile ? (
        // Item 21: below the breakpoint, the sidebar-vs-content split
        // stops being a resizable panel -- shadcn's Sidebar primitive
        // already renders itself as a Sheet overlay once useIsMobile()
        // (which it reads internally too) flips, so `sidebarElement` here
        // contributes zero layout width of its own; wrapping it in a
        // ResizablePanel (as the desktop branch does) would reserve
        // SIDEBAR_MIN_WIDTH of empty space for a component that's actually
        // rendering into a portal. The content area gets the full width
        // instead of splitting it with an invisible panel.
        <div className="flex h-svh w-full flex-col">
          {sidebarElement}
          <SidebarInset className="min-h-0 flex-1">
            {headerElement}
            {mainContentElement}
          </SidebarInset>
        </div>
      ) : (
        <ResizablePanelGroup orientation="horizontal" className="h-svh w-full">
          {/*
           * The sidebar-vs-content split itself -- the whole point of Item 6.
           * defaultSize/minSize/maxSize take plain numbers as pixels
           * directly (react-resizable-panels only treats unitless *strings*
           * as percentages), so SIDEBAR_MIN_WIDTH/SIDEBAR_MAX_WIDTH (12rem/
           * 32rem) apply as-is -- dragging past either bound still can't
           * collapse the sidebar to 0 or push it off-screen.
           */}
          <ResizablePanel
            panelRef={sidebarPanelRef}
            defaultSize={initialSidebarWidth}
            minSize={SIDEBAR_MIN_WIDTH}
            maxSize={SIDEBAR_MAX_WIDTH}
            collapsible
            collapsedSize={SIDEBAR_ICON_WIDTH}
            onResize={(size) => {
              // Dragging the handle below minSize snaps a collapsible
              // panel straight to collapsedSize (react-resizable-panels'
              // own behavior) without going through the `sidebarOpen`
              // effect above -- reflect that back into shadcn's Sidebar
              // context so its icon-rail CSS actually kicks in, instead of
              // squeezing the full expanded layout into 48px.
              if (sidebarPanelRef.current?.isCollapsed()) {
                setSidebarOpen(false);
                return;
              }
              setSidebarOpen(true);
              setSidebarWidth(size.inPixels);
            }}
            className="min-w-0"
          >
            {sidebarElement}
          </ResizablePanel>
          {/*
           * react-resizable-panels' Separator always sets its own
           * `data-testid` (and `id`) to its resolved `id` prop, clobbering
           * any `data-testid` passed directly -- so the only way to control
           * the rendered data-testid here is via `id`, not `data-testid`.
           */}
          <ResizableHandle withHandle id="sidebar-resize-handle" />
          <ResizablePanel minSize={30} className="min-w-0">
            {/*
             * Panel's own box gets its height from the group's flex-stretch,
             * not from content -- SidebarInset's <main> needs an explicit
             * h-full to actually fill it (it's a plain block child of Panel
             * now, not a flex sibling of the sidebar the way it was before
             * Item 6's restructuring), which is what every pane under it
             * (TaskDetailPane's own "flex h-full flex-col" root, etc.) relies
             * on to size its scrollable region correctly.
             */}
            <SidebarInset className="h-full">
              {headerElement}
              {mainContentElement}
            </SidebarInset>
          </ResizablePanel>
        </ResizablePanelGroup>
      )}
      <QuickOpen
        client={client}
        task={selectedTask}
        open={quickOpenOpen}
        onOpenChange={setQuickOpenOpen}
        onOpenFile={openFileTab}
        events={events}
      />
    </SidebarProvider>
  );
}

/**
 * The shell's own command-palette contributions: tasks, workspaces, the
 * selected task's tabs and changed files, and the theme action.
 *
 * It is a component rather than a block inside AppShell so each source's
 * `useMemo` sits next to the data it derives from, and so nothing here
 * re-renders the shell. Other surfaces contribute their own sources the
 * same way -- `app-sidebar.tsx` registers its create/accounts dialogs
 * without this file knowing about them (see `docs/design.md` §9).
 */
function ShellCommands({
  client,
  tasks,
  workspaces,
  selectedTask,
  tabs,
  onSelectTask,
  onOpenTab,
  onActivateTab,
}: {
  client: WsClient | null;
  tasks: Task[];
  workspaces: Workspace[];
  selectedTask: Task | null;
  tabs: TabEntry[] | null;
  onSelectTask: (task: Task) => void;
  onOpenTab: (taskId: number, entry: TabEntry, placement?: TabPlacement) => void;
  onActivateTab: (taskId: number, key: string) => void;
}) {
  const { preference, setPreference } = useTheme();

  const taskCommands = useMemo<Command[]>(
    () =>
      tasks.map((task) => ({
        id: `task-${task.ID}`,
        group: "Tasks",
        title: task.Title,
        subtitle: task.Branch ?? undefined,
        keywords: [task.Status],
        run: () => onSelectTask(task),
      })),
    [tasks, onSelectTask],
  );
  useCommands("shell:tasks", 0, taskCommands);

  const workspaceCommands = useMemo<Command[]>(
    () =>
      workspaces
        .map((workspace): Command | null => {
          // smind has no "selected workspace" in the shell -- selection is
          // per task (ADR 0004) -- so a workspace entry lands on its first
          // task. A workspace with no tasks has nothing to land on, so it
          // contributes no entry rather than a row that does nothing.
          const first = tasks.find((t) => t.WorkspaceID === workspace.ID);
          if (!first) return null;
          return {
            id: `workspace-${workspace.ID}`,
            group: "Workspaces",
            title: workspace.Title || workspace.Path,
            subtitle: workspace.Path,
            run: () => onSelectTask(first),
          };
        })
        .filter((c): c is Command => c !== null),
    [workspaces, tasks, onSelectTask],
  );
  useCommands("shell:workspaces", 1, workspaceCommands);

  const tabCommands = useMemo<Command[]>(() => {
    if (!selectedTask) return [];
    // Every base tab kind, whether or not it's currently open: "Open
    // Terminal" should work after the tab was closed, which is the case a
    // list built from `tabs` alone would miss.
    const base = defaultTabsForTask(selectedTask.ID);
    const open = new Map((tabs ?? []).map((t) => [t.key, t]));
    return base.map((entry) => ({
      id: `tab-${entry.kind}`,
      group: "Open",
      title: `Open ${TAB_KINDS[entry.kind].defaultTitle}`,
      subtitle: selectedTask.Title,
      keywords: [entry.kind],
      run: () => {
        if (open.has(entry.key)) onActivateTab(selectedTask.ID, entry.key);
        else onOpenTab(selectedTask.ID, entry, "prefer");
      },
    }));
  }, [selectedTask, tabs, onOpenTab, onActivateTab]);
  useCommands("shell:tabs", 2, tabCommands);

  const files = useTaskChangedFiles(client, selectedTask);
  const fileCommands = useMemo<Command[]>(() => {
    if (!selectedTask) return [];
    return files.map((path) => ({
      id: `file-${path}`,
      group: "Files",
      title: path.split("/").pop() || path,
      subtitle: path,
      keywords: [path],
      run: () => onOpenTab(selectedTask.ID, fileTab(selectedTask.ID, path), "prefer"),
    }));
  }, [files, selectedTask, onOpenTab]);
  useCommands("shell:files", 3, fileCommands);

  const actionCommands = useMemo<Command[]>(
    () => [
      {
        id: "cycle-theme",
        group: "Actions",
        title: "Cycle theme",
        subtitle: `Currently ${preference}`,
        keywords: ["dark mode", "light mode", "appearance"],
        action: "theme.cycle",
        run: () => {
          const order: ThemePreference[] = ["light", "dark", "system"];
          setPreference(order[(order.indexOf(preference) + 1) % order.length]!);
        },
      },
    ],
    [preference, setPreference],
  );
  useCommands("shell:actions", 4, actionCommands);

  return null;
}

/**
 * The selected task's changed file paths, for the palette's Files group.
 *
 * `task.files` is the task's *diff* -- the files it has touched -- not an
 * index of the worktree. That is the whole of what the wire offers today
 * (there is no recursive list or search RPC; `file.list` is one directory
 * per call), and it is also the more useful set for a palette: the files
 * of the task you're in. A full worktree index needs a daemon change and
 * belongs to Item 18, which is where the plan already puts the
 * measure-before-adding-an-RPC decision.
 *
 * Failures are swallowed to an empty list: the palette must still open
 * and show its other groups when a task's diff can't be read.
 */
function useTaskChangedFiles(client: WsClient | null, task: Task | null): string[] {
  const [files, setFiles] = useState<string[]>([]);

  useEffect(() => {
    if (!client || !task) {
      setFiles([]);
      return;
    }
    let cancelled = false;
    client
      .call<TaskFilesResult>("task.files", { taskId: task.ID })
      .then((result) => {
        if (!cancelled) setFiles((result?.files ?? []).map((f) => f.path));
      })
      .catch(() => {
        if (!cancelled) setFiles([]);
      });
    return () => {
      cancelled = true;
    };
  }, [client, task]);

  return files;
}

/** Claims `sidebar.toggle` from inside SidebarProvider (where `useSidebar()` is legal) and renders nothing. */
function SidebarToggleAction() {
  const { toggleSidebar } = useSidebar();
  useActionHandler("sidebar.toggle", toggleSidebar);
  return null;
}

/**
 * Everything a leaf pane needs to render its tab strip + content, shared
 * unchanged across every pane in the tree -- only `paneId`/`tabs`/
 * `activeKey`/`paneCount` vary per pane (see {@link SplitTreeView}).
 */
interface SplitPaneCallbacks {
  task: Task;
  client: WsClient | null;
  connectionStatus: ConnectionStatus;
  events: ReturnType<typeof useDaemonEvents>;
  onOpenFile: (path: string) => void;
  onOpenFileToSide: (path: string) => void;
  onActivate: (key: string) => void;
  onClose: (key: string) => void;
  /** Item 6's tab context menu: close every other closable tab in key's pane, or every closable tab to its left/right. */
  onCloseOthers: (key: string) => void;
  onCloseLeft: (key: string) => void;
  onCloseRight: (key: string) => void;
  /** Item 6's "Rename" (terminal tabs only -- see DraggableTabTrigger). */
  onRenameTab: (key: string, title: string) => void;
  /** Splits `key`'s tab off of `paneId` in `direction` -- Item 3's affordance, threaded down so each pane can bind its own id at the point it's rendered. */
  onSplitTab: (paneId: string, key: string, direction: SplitDirection) => void;
  /** Persists a resize handle drag's new sizes for the group with `groupId`. */
  onResizeGroup: (groupId: string, sizes: number[]) => void;
  onRevealInDiff: () => void;
  onNewTerminal: () => void;
  /** Opens (or activates) one of the base tab kinds -- Item 3's "+" menu and empty-state buttons. */
  onOpenBase: (kind: BaseTabKind, placement: TabPlacement) => void;
  /** The pane a drag is currently hovering over, if any (Item 8) -- drives which pane renders its drop-zone overlay. */
  dragOverPaneId: string | null;
  /** The zone within `dragOverPaneId` the drag is over -- `null` whenever `dragOverPaneId` is. */
  dropPosition: SplitDropZonePosition | null;
  /** Whether a tab drag is in progress at all -- panes stay droppable-but-invisible until one starts. */
  isDragActive: boolean;
  /** Which pane keyboard actions (tab.close, pane.focus.*, ...) target -- Item 6's focused-pane concept. Drives the visible ring below. */
  focusedPaneId: string | null;
  /** Moves keyboard focus to a pane -- bound to a click/pointerdown anywhere in it, same as clicking a pane in most split-pane editors. */
  onFocusPane: (paneId: string) => void;
  /** The pane whose "+" menu `tab.new` popped open, if any -- see NewTabButton's controlled open/onOpenChange. */
  openNewTabPaneId: string | null;
  onOpenNewTabPaneIdChange: (paneId: string | null) => void;
}

function splitNodeId(node: SplitNode): string {
  return node.kind === "pane" ? node.pane.id : node.group.id;
}

/**
 * The recursive renderer for a task's split tree (Item 4): a `"pane"` node
 * is a leaf -- {@link PaneTabStrip} -- and a `"group"` node is a
 * `ResizablePanelGroup` wrapping one `ResizablePanel` per child, recursing.
 * `react-resizable-panels` nests `ResizablePanelGroup`s arbitrarily -- this
 * is its own documented pattern, not new capability being added here.
 *
 * This component itself never calls a hook conditionally: the group-only
 * `useInitialValue` call lives in {@link SplitGroupView}, a distinct
 * component the "pane" branch never renders, so a pane converting to a
 * group (a fresh split) is an ordinary "different component type at this
 * position" remount, not a rules-of-hooks violation.
 */
function SplitTreeView({
  node,
  paneCount,
  ...shared
}: { node: SplitNode; paneCount: number } & SplitPaneCallbacks) {
  if (node.kind === "pane") {
    return (
      <PaneTabStrip
        paneId={node.pane.id}
        tabs={node.pane.tabs}
        activeKey={node.pane.activeKey}
        paneCount={paneCount}
        task={shared.task}
        client={shared.client}
        connectionStatus={shared.connectionStatus}
        events={shared.events}
        onOpenFile={shared.onOpenFile}
        onOpenFileToSide={shared.onOpenFileToSide}
        onActivate={shared.onActivate}
        onClose={shared.onClose}
        onCloseOthers={shared.onCloseOthers}
        onCloseLeft={shared.onCloseLeft}
        onCloseRight={shared.onCloseRight}
        onRenameTab={shared.onRenameTab}
        onSplit={(key, direction) => shared.onSplitTab(node.pane.id, key, direction)}
        onRevealInDiff={shared.onRevealInDiff}
        onNewTerminal={shared.onNewTerminal}
        onOpenBase={shared.onOpenBase}
        dragOverPaneId={shared.dragOverPaneId}
        dropPosition={shared.dropPosition}
        isDragActive={shared.isDragActive}
        focused={shared.focusedPaneId === node.pane.id}
        onFocusPane={() => shared.onFocusPane(node.pane.id)}
        newTabMenuOpen={shared.openNewTabPaneId === node.pane.id}
        onNewTabMenuOpenChange={(open) => shared.onOpenNewTabPaneIdChange(open ? node.pane.id : null)}
      />
    );
  }
  return <SplitGroupView node={node} paneCount={paneCount} {...shared} />;
}

function SplitGroupView({
  node,
  paneCount,
  ...shared
}: { node: Extract<SplitNode, { kind: "group" }>; paneCount: number } & SplitPaneCallbacks) {
  // Frozen at mount (per group id) for the same reason `initialSidebarWidth`
  // is frozen above: react-resizable-panels' `defaultLayout` is the
  // group's *initial* layout only, and re-registers every panel (fighting
  // an in-progress drag) if the prop's value changes on every resize
  // frame. A resize's own live sizes still reach `resizeSplitInLayout`
  // via `onLayoutChange` below -- this only stops that write-back from
  // feeding back into the prop that seeded this render.
  const initialLayout = useInitialValue(
    Object.fromEntries(
      node.group.children.map((child, index) => [splitNodeId(child), (node.group.sizes[index] ?? 0) * 100]),
    ),
    node.group.id,
  );

  return (
    <ResizablePanelGroup
      id={node.group.id}
      orientation={node.group.direction === "horizontal" ? "horizontal" : "vertical"}
      className="h-full w-full"
      defaultLayout={initialLayout}
      onLayoutChange={(layout) => {
        const sizes = node.group.children.map((child) => (layout[splitNodeId(child)] ?? 0) / 100);
        shared.onResizeGroup(node.group.id, sizes);
      }}
    >
      {node.group.children.map((child, index) => (
        <Fragment key={splitNodeId(child)}>
          {index > 0 && <ResizableHandle withHandle />}
          <ResizablePanel id={splitNodeId(child)} minSize={`${MIN_SPLIT_SIZE * 100}%`} className="min-w-0">
            <SplitTreeView node={child} paneCount={paneCount} {...shared} />
          </ResizablePanel>
        </Fragment>
      ))}
    </ResizablePanelGroup>
  );
}

/**
 * One pane's tab strip + content: title, a "Split" affordance on movable
 * kinds (file/diff/terminal -- Chat and Files stay pinned, per Item 6),
 * and the existing close affordance on closable kinds. Shared across
 * every pane in the tree rather than written per-pane, parameterized by
 * `paneId` only for which pane a split/open-base action targets.
 */
function PaneTabStrip({
  paneId,
  tabs,
  activeKey,
  paneCount,
  task,
  client,
  connectionStatus,
  events,
  onOpenFile,
  onOpenFileToSide,
  onActivate,
  onClose,
  onCloseOthers,
  onCloseLeft,
  onCloseRight,
  onRenameTab,
  onSplit,
  onRevealInDiff,
  onNewTerminal,
  onOpenBase,
  showMoveAffordance = true,
  dragOverPaneId = null,
  dropPosition = null,
  isDragActive = false,
  focused = false,
  onFocusPane,
  newTabMenuOpen = false,
  onNewTabMenuOpenChange,
}: {
  paneId: PaneId;
  tabs: TabEntry[];
  activeKey: string | null;
  /** How many panes exist in the tree right now -- only used to pick the outer root's `data-testid` (see below). */
  paneCount: number;
  task: Task;
  client: WsClient | null;
  connectionStatus: ConnectionStatus;
  events: ReturnType<typeof useDaemonEvents>;
  onOpenFile: (path: string) => void;
  onOpenFileToSide: (path: string) => void;
  onActivate: (key: string) => void;
  onClose: (key: string) => void;
  onCloseOthers: (key: string) => void;
  onCloseLeft: (key: string) => void;
  onCloseRight: (key: string) => void;
  onRenameTab: (key: string, title: string) => void;
  onSplit: (key: string, direction: SplitDirection) => void;
  onRevealInDiff: () => void;
  onNewTerminal: () => void;
  /** Opens (or activates) one of the base tab kinds -- Item 3's "+" menu and empty-state buttons. */
  onOpenBase: (kind: BaseTabKind, placement: TabPlacement) => void;
  /** Item 21: the split tree is a desktop-only concept -- compact has nowhere for "split" to open a second pane, so it's hidden rather than left to open a split that never renders. */
  showMoveAffordance?: boolean;
  /** Item 8: only set (by the DndContext-wrapped desktop branch) while a tab drag is over *this* pane. Compact mode never passes these -- it has no DndContext ancestor, so `useDraggable`/`useDroppable` below are inert there anyway. */
  dragOverPaneId?: string | null;
  dropPosition?: SplitDropZonePosition | null;
  isDragActive?: boolean;
  /** Item 6: whether this is the pane keyboard actions currently target -- drives the visible ring. Compact mode (a single merged strip) never sets this; there's nothing to distinguish it from. */
  focused?: boolean;
  /** Moves keyboard focus here -- bound to a pointerdown anywhere in the pane. */
  onFocusPane?: () => void;
  newTabMenuOpen?: boolean;
  onNewTabMenuOpenChange?: (open: boolean) => void;
}) {
  // Item 8: the whole pane (tab strip + content) is one drop target --
  // `data` carries just the pane id, which `App.tsx`'s onDragMove/onDragEnd
  // read back off `event.over`.
  const { setNodeRef: setDroppableRef } = useDroppable({ id: paneId, data: { paneId } });
  const showDropPreview = isDragActive && dragOverPaneId === paneId && dropPosition !== null;

  return (
    <div
      ref={setDroppableRef}
      // A ring rather than a border: a border would shift every pane's
      // content by its width when focus moves, and only means anything
      // once there's more than one pane to tell apart.
      className={cn("relative h-full", focused && paneCount > 1 && "ring-1 ring-inset ring-ring")}
      data-testid={focused ? "pane-focused" : undefined}
      onPointerDownCapture={onFocusPane}
    >
      <Tabs
        key={`${task.ID}:${paneId}`}
        // The fixed `primary-pane`/`side-pane` testids only tell the two
        // panes of today's common 0-or-1-split case apart; a 3rd+ pane has
        // no fixed name, so `data-pane-id` below is what a test targeting
        // it uses instead.
        data-testid={paneId === DEFAULT_PANE_ID ? "primary-pane" : paneCount === 2 ? "side-pane" : undefined}
        data-pane-id={paneId}
        value={activeKey ?? undefined}
        onValueChange={onActivate}
        className="h-full gap-0"
      >
        <div className="mx-3 mt-2 flex items-center gap-1 overflow-x-auto">
          {tabs.length > 0 && (
            <TabsList className="w-fit">
              {tabs.map((entry, index) => (
                <DraggableTabTrigger
                  key={entry.key}
                  entry={entry}
                  paneId={paneId}
                  showMoveAffordance={showMoveAffordance}
                  onSplit={onSplit}
                  onClose={onClose}
                  onCloseOthers={onCloseOthers}
                  onCloseLeft={onCloseLeft}
                  onCloseRight={onCloseRight}
                  onRenameTab={onRenameTab}
                  hasOtherClosableTabs={tabs.some((t) => t.key !== entry.key && t.closable)}
                  hasClosableTabsToLeft={tabs.slice(0, index).some((t) => t.closable)}
                  hasClosableTabsToRight={tabs.slice(index + 1).some((t) => t.closable)}
                />
              ))}
            </TabsList>
          )}
          <NewTabButton
            onOpen={(kind) => onOpenBase(kind, { pane: paneId })}
            open={newTabMenuOpen}
            onOpenChange={onNewTabMenuOpenChange}
          />
        </div>
        {tabs.length === 0 && (
          <TabsEmptyState onOpen={(kind) => onOpenBase(kind, { pane: paneId })} />
        )}
        {tabs.map((entry) => (
          /*
           * Terminal tabs force-mount (and hide when inactive) so a
           * backgrounded terminal keeps streaming into its buffer -- which
           * is what lets its tab show an activity dot, and what stops
           * switching panes or tabs from dropping output on the floor
           * (Item 20). The detach-not-close contract is unchanged: the
           * pane still aborts its attach, and never calls terminal.close,
           * when it genuinely unmounts (tab closed, task switched).
           */
          <TabsContent
            key={entry.key}
            value={entry.key}
            forceMount={entry.kind === "terminal" ? true : undefined}
            className="min-h-0 data-[state=inactive]:hidden"
          >
            <TabContent
              entry={entry}
              client={client}
              task={task}
              active={activeKey === entry.key}
              connectionStatus={connectionStatus}
              onOpenFile={onOpenFile}
              onOpenFileToSide={onOpenFileToSide}
              onRevealInDiff={onRevealInDiff}
              onNewTerminal={onNewTerminal}
              events={events}
            />
          </TabsContent>
        ))}
      </Tabs>
      {showDropPreview && <SplitDropPreview position={dropPosition!} />}
    </div>
  );
}

/**
 * One tab strip entry: the tab trigger itself plus its "Split" and close
 * affordances -- pulled out of {@link PaneTabStrip}'s tab list into its own
 * component because `useDraggable` (Item 8) is a hook, and hooks can't be
 * called once per array element inside another component's render body.
 */
function DraggableTabTrigger({
  entry,
  paneId,
  showMoveAffordance,
  onSplit,
  onClose,
  onCloseOthers,
  onCloseLeft,
  onCloseRight,
  onRenameTab,
  hasOtherClosableTabs,
  hasClosableTabsToLeft,
  hasClosableTabsToRight,
}: {
  entry: TabEntry;
  paneId: string;
  showMoveAffordance: boolean;
  onSplit: (key: string, direction: SplitDirection) => void;
  onClose: (key: string) => void;
  onCloseOthers: (key: string) => void;
  onCloseLeft: (key: string) => void;
  onCloseRight: (key: string) => void;
  onRenameTab: (key: string, title: string) => void;
  hasOtherClosableTabs: boolean;
  hasClosableTabsToLeft: boolean;
  hasClosableTabsToRight: boolean;
}) {
  // Rename (terminal tabs only -- their title is arbitrary already, unlike
  // a file/diff/chat tab's, which is derived from real identity a cosmetic
  // override would just make misleading) swaps the trigger for a plain
  // input in the same slot rather than trying to nest one inside
  // TabsTrigger's own <button> -- same reasoning as the close "×" below
  // being a span, not a button, but an <input> genuinely can't go inside
  // one at all.
  const [renaming, setRenaming] = useState(false);
  const cancelledRenameRef = useRef(false);
  // Every tab is draggable, not just movable kinds -- dropping a Chat/Files
  // tab onto another pane's center still moves it there (Item 8's scope);
  // only *splitting* (an edge drop, handled in App.tsx's onDragEnd) is
  // gated to isMovableKind, matching the "Split" menu below.
  //
  // `useDraggable`'s `attributes` (role="button", aria-roledescription,
  // tabIndex, ...) are meant for making an otherwise-plain element
  // accessible as a draggable widget -- TabsTrigger already has a proper,
  // keyboard-operable `role="tab"` via Radix, and spreading `attributes`
  // on top overwrites that role with dnd-kit's generic "button", breaking
  // `getByRole("tab", ...)` lookups (verified: it did, in this file's own
  // existing tests). Only `listeners` (the pointer handlers that actually
  // start a drag) are spread here.
  const { listeners, setNodeRef } = useDraggable({
    id: entry.key,
    data: { tabKey: entry.key, sourcePaneId: paneId, kind: entry.kind } satisfies DraggedTabData,
  });

  if (renaming) {
    return (
      <input
        autoFocus
        defaultValue={entry.title}
        aria-label={`Rename ${entry.title}`}
        data-testid="workspace-tab-rename-input"
        className="h-7 max-w-48 shrink-0 rounded border border-ring bg-transparent px-2 text-ui-base outline-none"
        onFocus={(e) => e.currentTarget.select()}
        onKeyDown={(e) => {
          e.stopPropagation();
          if (e.key === "Enter") {
            e.currentTarget.blur();
          } else if (e.key === "Escape") {
            cancelledRenameRef.current = true;
            setRenaming(false);
          }
        }}
        onBlur={(e) => {
          if (!cancelledRenameRef.current) onRenameTab(entry.key, e.currentTarget.value);
          cancelledRenameRef.current = false;
          setRenaming(false);
        }}
      />
    );
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <TabsTrigger
          ref={setNodeRef}
          value={entry.key}
          data-testid={`workspace-tab-${entry.kind}`}
          className="max-w-48 gap-1.5"
          {...listeners}
        >
          <TabLabel entry={entry} />
      {isMovableKind(entry.kind) && showMoveAffordance && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <span
              role="button"
              tabIndex={0}
              aria-label={`Split ${entry.title}`}
              data-testid="workspace-tab-split"
              data-tab-key={entry.key}
              onClick={(e) => {
                e.stopPropagation();
                e.preventDefault();
              }}
              className="flex shrink-0 items-center rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none"
            >
              {/* aria-hidden: this span's own aria-label already names it; without
                  hiding the icon too, it would be announced a second time as
                  part of the *ancestor* TabsTrigger's accessible name. */}
              <Columns2 aria-hidden="true" className="size-3" />
            </span>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem data-testid="workspace-tab-split-left" onClick={() => onSplit(entry.key, "left")}>
              Split left
            </DropdownMenuItem>
            <DropdownMenuItem data-testid="workspace-tab-split-right" onClick={() => onSplit(entry.key, "right")}>
              Split right
            </DropdownMenuItem>
            <DropdownMenuItem data-testid="workspace-tab-split-up" onClick={() => onSplit(entry.key, "up")}>
              Split up
            </DropdownMenuItem>
            <DropdownMenuItem data-testid="workspace-tab-split-down" onClick={() => onSplit(entry.key, "down")}>
              Split down
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {entry.closable && (
        <span
          role="button"
          tabIndex={0}
          aria-label={`Close ${entry.title}`}
          data-testid="workspace-tab-close"
          data-tab-key={entry.key}
          onClick={(e) => {
            e.stopPropagation();
            e.preventDefault();
            onClose(entry.key);
          }}
          // A `role="button"` element gets none of a real
          // <button>'s key handling for free, so Enter and Space
          // are wired explicitly (uiux-audit.md §4 P1 item 9). It
          // stays a span rather than becoming a <button> because
          // it sits inside Radix's TabsTrigger, which is already
          // a button -- nesting one inside another is invalid
          // HTML and React warns about it.
          onKeyDown={(e) => {
            if (e.key !== "Enter" && e.key !== " ") return;
            e.stopPropagation();
            e.preventDefault();
            onClose(entry.key);
          }}
          className="rounded px-1 text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none"
        >
          <span aria-hidden="true">×</span>
        </span>
      )}
        </TabsTrigger>
      </ContextMenuTrigger>
      <ContextMenuContent data-testid="workspace-tab-context-menu" data-tab-key={entry.key}>
        <ContextMenuItem
          disabled={!entry.closable}
          onSelect={() => onClose(entry.key)}
          data-testid="workspace-tab-menu-close"
        >
          Close
        </ContextMenuItem>
        <ContextMenuItem
          disabled={!hasOtherClosableTabs}
          onSelect={() => onCloseOthers(entry.key)}
          data-testid="workspace-tab-menu-close-others"
        >
          Close others
        </ContextMenuItem>
        <ContextMenuItem
          disabled={!hasClosableTabsToLeft}
          onSelect={() => onCloseLeft(entry.key)}
          data-testid="workspace-tab-menu-close-left"
        >
          Close to the left
        </ContextMenuItem>
        <ContextMenuItem
          disabled={!hasClosableTabsToRight}
          onSelect={() => onCloseRight(entry.key)}
          data-testid="workspace-tab-menu-close-right"
        >
          Close to the right
        </ContextMenuItem>
        {(entry.kind === "terminal" || (entry.kind === "file" && entry.path)) && <ContextMenuSeparator />}
        {entry.kind === "terminal" && (
          <ContextMenuItem onSelect={() => setRenaming(true)} data-testid="workspace-tab-menu-rename">
            <Pencil />
            Rename
          </ContextMenuItem>
        )}
        {entry.kind === "file" && entry.path && (
          <ContextMenuItem
            onSelect={() => {
              void navigator.clipboard?.writeText(entry.path!).catch(() => {});
            }}
            data-testid="workspace-tab-menu-copy-path"
          >
            <Copy />
            Copy path
          </ContextMenuItem>
        )}
        {isMovableKind(entry.kind) && showMoveAffordance && (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => onSplit(entry.key, "left")} data-testid="workspace-tab-menu-split-left">
              <Columns2 />
              Split left
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "right")} data-testid="workspace-tab-menu-split-right">
              <Columns2 />
              Split right
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "up")} data-testid="workspace-tab-menu-split-up">
              <Columns2 />
              Split up
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "down")} data-testid="workspace-tab-menu-split-down">
              <Columns2 />
              Split down
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  );
}

/**
 * The drop-shape preview overlay for a pane currently under a tab drag
 * (Item 8) -- a translucent highlight over the whole pane for a center
 * drop, or over the half the resulting split would occupy for an edge
 * drop. Reuses the same `primary` accent every other emphasis affordance
 * in this codebase's button/ring styling already uses (docs/design.md's
 * static token scale) rather than introducing a new color.
 */
function SplitDropPreview({ position }: { position: SplitDropZonePosition }) {
  return (
    <div
      data-testid="split-drop-preview"
      data-drop-position={position}
      className={cn(
        "pointer-events-none absolute z-40 rounded-md border-2 border-primary bg-primary/20",
        position === "center" && "inset-2",
        position === "left" && "inset-y-0 left-0 w-1/2",
        position === "right" && "inset-y-0 right-0 w-1/2",
        position === "top" && "inset-x-0 top-0 h-1/2",
        position === "bottom" && "inset-x-0 bottom-0 h-1/2",
      )}
    />
  );
}

function TabContent({
  entry,
  client,
  task,
  active,
  connectionStatus,
  onOpenFile,
  onOpenFileToSide,
  onRevealInDiff,
  onNewTerminal,
  events,
}: {
  entry: TabEntry;
  client: WsClient | null;
  task: Task;
  /** Whether this tab is the one in front -- only the force-mounted terminal panes can be rendered while false. */
  active: boolean;
  connectionStatus: ConnectionStatus;
  onOpenFile: (path: string) => void;
  onOpenFileToSide: (path: string) => void;
  onRevealInDiff: () => void;
  onNewTerminal: () => void;
  events: ReturnType<typeof useDaemonEvents>;
}) {
  const renderers: Record<TabKind, React.ReactNode> = {
    task: (
      <TaskDetailPane
        client={client}
        task={task}
        connectionStatus={connectionStatus}
        onOpenFile={onOpenFile}
        onOpenDiffTab={onRevealInDiff}
      />
    ),
    files: (
      <FileExplorerPane
        client={client}
        task={task}
        onOpenFile={onOpenFile}
        onOpenFileToSide={onOpenFileToSide}
        onRevealInDiff={onRevealInDiff}
        events={events}
      />
    ),
    file: <FileEditorPane client={client} task={task} path={filePathFromTabKey(entry.key)} events={events} />,
    diff: <DiffViewerPane client={client} task={task} events={events} />,
    terminal: (
      <TerminalPane
        client={client}
        task={task}
        tabKey={entry.key}
        active={active}
        connectionStatus={connectionStatus}
        onNewTerminal={onNewTerminal}
      />
    ),
  };
  return renderers[entry.kind];
}
