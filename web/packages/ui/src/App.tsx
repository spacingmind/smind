import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Icons } from "@/lib/icons";
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
import { TabRenameInput } from "@/components/tab-rename-input";
import { ArchiveChatDialog } from "@/components/crud-dialogs";
import { UsageModalRoot } from "@/components/account-usage/usage-modal";
import { CommandPalette } from "@/components/command-palette";
import { DesktopDaemonBanner } from "@/components/desktop-daemon-banner";
import { DesktopUpdateDialogView } from "@/components/desktop-update-dialog";
import { DesktopUnreachable } from "@/components/desktop-unreachable";
import { InboxScreen } from "@/components/inbox/inbox-screen";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { AppHeader } from "@/components/app-header";
import { UpdateBadge } from "@/components/update-badge";
import { HomeLanding } from "@/components/home/home-landing";
import { requestNewWorkspace } from "@/lib/app-events";
import { useAppUpdate } from "@/lib/app-update";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { PRTabAutoOpen } from "@/components/pr-panel/pr-tab-auto-open";
import { PluginPanelPane } from "@/components/plugin-panel/plugin-panel-pane";
import { PluginActionsProvider, type PluginActions } from "@/components/plugin-panel/plugin-actions-context";
import { PluginPage } from "@/components/plugin-panel/plugin-page";
import { useTaskGit } from "@/hooks/use-task-git";
import { PRPanelPane } from "@/components/pr-panel/pr-panel-pane";
import { PluginTabReaper } from "@/hooks/use-plugin-tab-reaper";
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
import { ResizableHandle, ResizablePanel, ResizablePanelGroup, SidebarResizeHandle } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  TAB_KINDS,
  baseTabForKind,
  chatTab,
  chatTabKey,
  defaultTabsForTask,
  prTab,
  fileTab,
  filePathFromTabKey,
  NewTabButton,
  nextTerminalTab,
  pluginTab,
  TabLabel,
  TabsEmptyState,
  type BaseTabKind,
  type ChatMenuEntry,
  type TabEntry,
  type TabKind,
} from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { PluginThemeApplier } from "@/components/plugin-theme-applier";
import { useSlashPaletteCommands } from "@/hooks/use-slash-palette-commands";
import { findPanel, usePluginPanels } from "@/hooks/use-plugin-panels";
import type { PluginPanel } from "@/lib/plugin-panel-types";
import { DaemonQueryProvider, useDaemonQueryClient } from "@/lib/query/daemon-query-provider";
import { openUsageModal } from "@/hooks/use-usage-modal";
import { useInitialValue } from "@/hooks/use-initial-value";
import { useIsMobile } from "@/hooks/use-mobile";
import { useFocusedChatStore } from "@/stores/focused-chat-store";
import { useTheme } from "@/hooks/use-theme";
import type { ActionId } from "@/keyboard/actions";
import { KeyboardProvider, useActionHandler, useActionRestriction } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useCommands, usePalette } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { useTaskChats } from "@/hooks/use-task-chats";
import { useUnreadTasks } from "@/hooks/use-unread-tasks";
import { isMovableKind, useTaskTabs, type PaneId, type SplitDirection, type TabPlacement } from "@/hooks/use-task-tabs";
import { useSettledSidebarOpen } from "@/hooks/use-settled-sidebar-open";
import { useSidebarResize } from "@/hooks/use-sidebar-resize";
import { SIDEBAR_ICON_WIDTH, SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidth } from "@/hooks/use-sidebar-width";
import { LoginScreen } from "@/components/login-screen";
import { LoginRequiredError, type LoginReason } from "@/lib/auth";
import { connectDaemon } from "@/lib/daemon";
import { desktop, desktopOS, isDesktop } from "@/lib/platform";
import { hasTrafficLights } from "@/lib/window-chrome";
import { useWindowState } from "@/hooks/use-window-state";
import { WindowSidebarToggle } from "@/components/window-sidebar-toggle";
import { watchForReconnect, type ConnectionStatus, type ReconnectHandle } from "@/lib/reconnect";
import { formatRoute, parseRoute, type Route } from "@/lib/route";
import { formatTabTitle } from "@/lib/tab-title";
import { SHELL_FRAME_INSET_CLASS, SHELL_FRAME_PANEL_CLASS } from "@/lib/shell-frame";
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
import type { Chat, Task, TaskFilesResult, Workspace } from "@/lib/types";
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
  //
  // Password gate: when the daemon wants a password (a browser on another
  // machine; loopback never gets here) AppShell reports it and this swaps
  // the whole shell for the login form. Unmounting the shell closes its
  // client and reconnect loop; after sign-in a bumped `attempt` key mounts a
  // fresh shell, which runs the normal initial connect with the new session.
  const [loginReason, setLoginReason] = useState<LoginReason | null>(null);
  const [attempt, setAttempt] = useState(0);
  if (loginReason !== null) {
    return (
      <LoginScreen
        reason={loginReason}
        onLoggedIn={() => {
          setLoginReason(null);
          setAttempt((n) => n + 1);
        }}
      />
    );
  }
  return (
    <KeyboardProvider>
      <PaletteProvider>
        <AppShell key={attempt} connect={connect} onLoginRequired={setLoginReason} />
      </PaletteProvider>
    </KeyboardProvider>
  );
}

/**
 * The only shortcut actions that dispatch while Settings covers the shell.
 * Everything else (task/tab/pane/chat navigation, new tab, quick open,
 * run.interrupt, sidebar toggle, ...) belongs to handlers that stay mounted
 * in the hidden shell and must not act on it unseen.
 */
const SETTINGS_SAFE_ACTIONS: readonly ActionId[] = [
  "palette.open",
  "shortcuts.help",
  "settings.open",
  "usage.open",
  "theme.cycle",
  "app.quit",
];

function AppShell({
  connect,
  onLoginRequired,
}: {
  connect: () => Promise<WsClient>;
  /** The daemon needs a password: the app swaps this shell for the login form. */
  onLoginRequired: (reason: LoginReason) => void;
}) {
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
  const [activeView, setActiveView] = useState<"workspace" | "settings" | "inbox" | "plugin">("workspace");
  // Bumped on every request to open Settings, and used as the screen's key:
  // asking for a section while Settings is already open (shortcuts.help,
  // a deep link) remounts it onto that section instead of being ignored.
  const [settingsOpenNonce, setSettingsOpenNonce] = useState(0);
  useActionRestriction(activeView !== "workspace" ? SETTINGS_SAFE_ACTIONS : null);
  const openSettings = useCallback((sectionId?: string) => {
    setSettingsInitialSectionId(sectionId ?? null);
    setSettingsOpenNonce((n) => n + 1);
    setActiveView("settings");
  }, []);
  const closeSettings = useCallback(() => setActiveView("workspace"), []);
  // The Human inbox (ADR-0045): a full-screen view like Settings, reached from the sidebar footer.
  // In the compact layout the sidebar is a sheet: let it finish closing before the inbox covers the screen, so
  // its exit animation never ghosts over the page.
  const openInbox = useCallback(() => {
    if (isMobile) {
      closeMobileSidebarRef.current?.();
      window.setTimeout(() => setActiveView("inbox"), 220);
    } else {
      setActiveView("inbox");
    }
  }, [isMobile]);
  // A plugin panel as a full page (ADR-0042): the surface for `where:
  // "sidebar"` panels and any panel opened with no task selected. Same
  // mobile treatment as the inbox: the sidebar sheet closes first so its
  // exit animation never ghosts over the page.
  const [pluginPage, setPluginPage] = useState<{ pluginId: string; panelId: string } | null>(null);
  const openPluginPage = useCallback(
    (pluginId: string, panelId: string) => {
      const show = () => {
        setPluginPage({ pluginId, panelId });
        setActiveView("plugin");
      };
      if (isMobile) {
        closeMobileSidebarRef.current?.();
        window.setTimeout(show, 220);
      } else {
        show();
      }
    },
    [isMobile],
  );

  const {
    tabsByTask,
    ensureTaskChats,
    updateChatTabTitle,
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
  const queryClient = useDaemonQueryClient();
  const events = useDaemonEvents(client, queryClient);
  const { attention, runStatus, rateLimit, runningChatsByTask, loaded: runsLoaded } = useTaskAttention(client, selectedTask?.ID ?? null, events);
  // ADR-0016 P3: the selected task's own chats, live -- the "+" menu's
  // reopen section, the seed-or-migrate step below, and every chat tab's
  // authoritative title/provider all read from this one fetch.
  const { chats } = useTaskChats(client, selectedTask?.ID ?? null, events, (chat) => {
    updateChatTabTitle(chat.TaskID, chat.ID, chat.Title);
  });
  // Seeds a freshly selected task's default-chat tab, or migrates a
  // persisted pre-ADR-0016 `${taskId}:task` tab onto it, the moment this
  // task's chats have actually loaded (see ensureTaskChats's own doc
  // comment for why it can't happen any earlier).
  useEffect(() => {
    // The chats[0].TaskID check is a defensive belt-and-suspenders against
    // useTaskChats ever serving a stale previous-task's chats array again
    // (it resets to null synchronously on taskId change precisely so this
    // never happens) -- seeding taskId's tab with a *different* task's
    // chat would be a real chat-identity mixup, not just a stale render.
    if (!selectedTask || !chats || chats.length === 0 || chats[0]!.TaskID !== selectedTask.ID) return;
    ensureTaskChats(selectedTask.ID, chats);
  }, [selectedTask, chats, ensureTaskChats]);
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
    if (typeof document !== "undefined") document.title = formatTabTitle("sMind", unread.size);
  }, [unread]);

  // The sidebar's user-resized width (px), persisted across reloads. It
  // only changes on commit (drag release / keyboard) -- the live drag is
  // written straight to the CSS var by `useSidebarResize`, never through
  // React state.
  const [sidebarWidth, setSidebarWidth] = useSidebarWidth();
  // Lifted `open` (SidebarProvider's controlled prop): the toggle button,
  // Ctrl+B and the palette flip it, and the sidebar panel just retargets
  // its width var so one CSS transition animates the change.
  const [sidebarOpen, setSidebarOpen] = useState(true);
  // One update controller, shared by the dialog and the header/sidebar badge (the launch check must run once).
  const appUpdate = useAppUpdate();
  const sidebarPanelRef = useRef<HTMLDivElement>(null);
  const sidebarResize = useSidebarResize({
    panelRef: sidebarPanelRef,
    width: sidebarWidth,
    onCommit: setSidebarWidth,
    onCollapse: () => setSidebarOpen(false),
  });
  // The sidebar's layout (expanded rows vs icon rail) trails `sidebarOpen` until
  // the panel's width animation ends; its content is hidden meanwhile so it is
  // never shown clipped or shifted mid-transition.
  const layoutOpen = useSettledSidebarOpen(sidebarOpen, sidebarPanelRef, !isMobile);
  const sidebarSettling = layoutOpen !== sidebarOpen;

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
          onLoginRequired: (reason) => {
            if (!cancelled) onLoginRequired(reason);
          },
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
        if (cancelled) return;
        if (err instanceof LoginRequiredError) {
          onLoginRequired(err.reason);
          return;
        }
        setConnectionStatus("disconnected");
        setConnectError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      cancelled = true;
      reconnectHandle?.close();
    };
  }, [connect, onLoginRequired]);

  // The task header's lineage jump (PR-U2): fetch the target task fresh
  // (the sidebar's tree may not hold an archived-then-reshown task) and
  // reuse the normal selection path so tabs/chat seeding behave exactly
  // like clicking it in the sidebar.
  const selectTask = useCallback((task: Task) => {
    setSelectedTask(task);
    // Tab seeding happens once this task's chats load (the effect above) --
    // useTaskChats is keyed on selectedTask.ID, so setting it here is what
    // kicks that fetch off.
  }, []);

  // Picking a task in the compact-layout sidebar sheet dismisses it (a no-op
  // on the desktop sidebar): the sheet would otherwise sit over the task the
  // user just opened, including when they re-pick the one already selected.
  const closeMobileSidebarRef = useRef<(() => void) | null>(null);
  const selectTaskFromSidebar = useCallback(
    (task: Task) => {
      selectTask(task);
      closeMobileSidebarRef.current?.();
    },
    [selectTask],
  );

  // The task header's lineage jump (PR-U2): fetch the target task fresh
  // (the sidebar's tree may not hold it) and reuse the normal selection
  // path so tabs/chat seeding behave exactly like a sidebar click.
  const jumpToTask = useCallback(
    async (taskId: number) => {
      if (!client) return;
      try {
        const target = await client.call<Task>("task.get", { id: taskId });
        selectTask(target);
      } catch (err) {
        console.error("task.get failed for lineage jump", err);
      }
    },
    [client, selectTask],
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

  /**
   * The one open path for a plugin panel (ADR-0042), shared by the "+" menu, the
   * command palette and the panel-actions provider: a sidebar panel -- or any
   * panel with no task selected -- opens the full page; every other panel opens
   * (or activates) its tab on the selected task. The same open-or-activate rule
   * as the base kinds: openTab itself activates an already-open `pluginTabKey`
   * instead of duplicating it, and `prefer` keeps the implicit-open placement.
   */
  const openPluginPanel = useCallback(
    (panel: PluginPanel) => {
      if (panel.where === "sidebar" || !selectedTask) {
        openPluginPage(panel.plugin, panel.id);
        return;
      }
      openTab(selectedTask.ID, pluginTab(selectedTask.ID, panel), "prefer");
    },
    [selectedTask, openTab, openPluginPage],
  );

  /** Opens another terminal tab for the selected task (Item 20). The pane picks its own session -- see lib/terminal-sessions.ts. */
  function openTerminalTab() {
    if (!selectedTask) return;
    const state = tabsByTask.get(selectedTask.ID);
    const tabs = state ? collectAllTabs(state.root) : [];
    openTab(selectedTask.ID, nextTerminalTab(selectedTask.ID, tabs));
  }

  /** The header's terminal button: bring an existing terminal tab forward, else open the first one. (The "+" menu and the palette are what open further terminals.) */
  function focusOrOpenTerminal() {
    if (!selectedTask) return;
    const state = tabsByTask.get(selectedTask.ID);
    const existing = (state ? collectAllTabs(state.root) : []).find((t) => t.kind === "terminal");
    if (existing) activate(selectedTask.ID, existing.key);
    else openTerminalTab();
  }

  // --- Chats (ADR-0016 P3) -----------------------------------------------

  /** Opens (or activates) chat's own tab -- the "+" menu's reopen section, the "Also running" chip's jump target, and the route restore effect below all funnel through here. */
  function openChatTab(chat: Pick<Chat, "ID" | "Title">, placement?: TabPlacement) {
    if (!selectedTask) return;
    openTab(selectedTask.ID, chatTab(selectedTask.ID, chat), placement ?? "prefer");
  }

  /** Looks chatId up in the selected task's already-fetched chats and opens it -- what the "+" menu's per-chat items and the "Also running" chip (which only knows a chatId, from run.status) actually call. */
  function openChatById(chatId: number, placement?: TabPlacement) {
    const chat = chats?.find((c) => c.ID === chatId);
    if (chat) {
      openChatTab(chat, placement);
      return;
    }
    // A chat just created elsewhere (a failover retry) can beat its chat.created event into `chats`.
    client
      ?.call<Chat>("chat.get", { id: chatId })
      .then((fetched) => openChatTab(fetched, placement))
      .catch((err: unknown) => console.error("chat.get failed", err));
  }

  /** "+" -> "New chat": creates a chat via chat.create and opens its tab immediately (chat.created's own event, subscribed by useTaskChats, updates the `chats` list independently -- no need to wait for it here). */
  function openNewChat(placement?: TabPlacement) {
    if (!selectedTask || !client) return;
    client
      .call<Chat>("chat.create", { taskId: selectedTask.ID })
      .then((chat) => openChatTab(chat, placement))
      .catch((err: unknown) => console.error("chat.create failed", err));
  }

  /** Inline chat-tab rename (App.tsx's renameTabForTask dispatches here for kind "chat"): updates the tab optimistically, then calls chat.rename -- best-effort, matching this codebase's other fire-and-forget RPC calls (e.g. provider.list's own catch). A failed rename leaves the optimistic title in place rather than reverting to a value the user has already moved past. */
  function renameChat(chatId: number, title: string) {
    if (!selectedTask) return;
    const trimmed = title.trim();
    if (!trimmed) return;
    updateChatTabTitle(selectedTask.ID, chatId, trimmed);
    client?.call("chat.rename", { id: chatId, title: trimmed }).catch((err: unknown) => console.error("chat.rename failed", err));
  }

  /** Which chat.archive confirmation is open, if any (Item: "Archive is an explicit action with a confirm"). */
  const [archivingChat, setArchivingChat] = useState<Pick<Chat, "ID" | "Title"> | null>(null);

  /** Detaches the archived chat's tab -- a stale tab into an archived chat has nothing left to do (chat.archive already refused it while running, so this only ever runs for a chat that's genuinely done). */
  function onChatArchived() {
    if (!selectedTask || !archivingChat) return;
    closeTab(selectedTask.ID, chatTabKey(selectedTask.ID, archivingChat.ID));
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
    if (!selectedTask || !taskState) return;
    const entry = collectAllTabs(taskState.root).find((t) => t.key === key);
    if (entry?.kind === "chat" && entry.chatId !== undefined) {
      renameChat(entry.chatId, title);
      return;
    }
    renameTab(selectedTask.ID, key, title);
  }

  function archiveChatTab(entry: TabEntry) {
    if (entry.chatId === undefined) return;
    setArchivingChat({ ID: entry.chatId, Title: entry.title });
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
  // Which of the selected task's chats already have a tab open, anywhere
  // in its split tree -- the "+" menu's reopen section (ADR-0016 P3) only
  // ever lists ones that don't, computed once here rather than per-pane
  // (a chat open in one pane must not also show up in another pane's "+").
  const openChatIds = useMemo(
    () =>
      new Set(
        (taskState ? collectAllTabs(taskState.root) : [])
          .filter((t): t is TabEntry & { chatId: number } => t.kind === "chat" && t.chatId !== undefined)
          .map((t) => t.chatId),
      ),
    [taskState],
  );

  // The chat the user is working in, for task-level surfaces with no chat of
  // their own (the Diff pane's "Submit review" addresses it -- G6). A chat
  // tab in front wins; while a non-chat tab (the Diff itself) is in front the
  // last focused chat stays, so long as its tab is still open; with none,
  // the first open chat tab.
  const setFocusedChat = useFocusedChatStore((state) => state.setFocusedChat);
  const selectedTaskId = selectedTask?.ID ?? null;
  useEffect(() => {
    if (selectedTaskId === null || !taskState) return;
    const panes = collectAllPanes(taskState.root);
    const chatInFront = (pane: (typeof panes)[number] | null) => {
      const front = pane?.tabs.find((t) => t.key === pane.activeKey);
      return front?.kind === "chat" && front.chatId !== undefined ? front.chatId : null;
    };
    const inFront = chatInFront(focusedPane) ?? panes.map(chatInFront).find((id) => id !== null) ?? null;
    const current = useFocusedChatStore.getState().byTask[selectedTaskId] ?? null;
    const open = collectAllTabs(taskState.root).filter((t) => t.kind === "chat" && t.chatId !== undefined);
    const stillOpen = current !== null && open.some((t) => t.chatId === current);
    setFocusedChat(selectedTaskId, inFront ?? (stillOpen ? current : (open[0]?.chatId ?? null)));
  }, [selectedTaskId, taskState, focusedPane, setFocusedChat]);

  // "Add to chat" (Diff / Files panes) queued a token for a chat: make sure that chat's composer is on screen.
  const revealRequest = useFocusedChatStore((state) => state.revealRequest);
  const handledReveal = useRef(revealRequest?.nonce);
  useEffect(() => {
    if (!revealRequest || revealRequest.nonce === handledReveal.current) return;
    handledReveal.current = revealRequest.nonce;
    if (!taskState || revealRequest.taskId !== selectedTaskId) return;
    const visible = collectAllPanes(taskState.root).some((pane) => {
      const front = pane.tabs.find((t) => t.key === pane.activeKey);
      return front?.kind === "chat" && front.chatId === revealRequest.chatId;
    });
    if (!visible) openChatById(revealRequest.chatId);
  });

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
      } else if (pendingRoute.tab.kind === "chat") {
        // The matched task's chats may not have loaded yet (useTaskChats
        // only starts fetching once selectTask above sets it as
        // selected) -- wait for them rather than degrading to nothing, so
        // this effect re-fires once `chats` resolves for this same task.
        if (chats === null) return;
        const wantedChatId = pendingRoute.tab.chatId;
        const targetChat = chats.find((c) => c.ID === wantedChatId);
        if (targetChat) openTab(match.ID, chatTab(match.ID, targetChat));
      } else if (pendingRoute.tab.kind === "pr") {
        openTab(match.ID, prTab(match.ID));
      } else {
        // Not just activate(): a deep link to e.g. .../diff must be able
        // to open that tab, not just activate an entry that may not exist
        // yet. openTab already no-ops to a plain activate when the tab is
        // already open.
        openTab(match.ID, baseTabForKind(match.ID, pendingRoute.tab.kind));
      }
      setPendingRoute(null);
    } else if (treeLoaded) {
      // The tree has answered and this task isn't in it (archived,
      // deleted, or never existed) -- degrade to the empty state rather
      // than waiting on a task that will never arrive.
      setPendingRoute(null);
    }
  }, [pendingRoute, allTasks, treeLoaded, chats, selectTask, openTab, activate]);

  useEffect(() => {
    if (!selectedTask) return;
    // With an arbitrary tree of panes there's no single "primary" story to
    // lean on any more -- whichever pane the user last focused is the only
    // rule that generalizes (see the plan's Decisions).
    const activeEntry = focusedPane?.tabs.find((t) => t.key === focusedPane.activeKey);
    // No tab open yet: the task's chats haven't loaded, so ensureTaskChats
    // hasn't had a chance to seed the default-chat tab (App.tsx's own
    // effect above, keyed on `chats`). Writing *any* hash here -- the old
    // code's "files" fallback included -- would round-trip through the
    // route-restore effect and openTab a tab that pre-empts that seeding,
    // permanently starving the task of its real default chat tab. Wait
    // for a real active tab instead; this effect re-fires once one exists.
    if (!activeEntry) return;
    // Plugin tabs are not routable: a panel's identity lives in the tab
    // entry, not the URL, so the previous hash stays until another tab is
    // focused.
    if (activeEntry.kind === "plugin") return;
    const tab: Route["tab"] =
      activeEntry.kind === "file"
        ? { kind: "file", path: filePathFromTabKey(activeEntry.key) }
        : activeEntry.kind === "chat" && activeEntry.chatId !== undefined
          ? { kind: "chat", chatId: activeEntry.chatId }
          : { kind: activeEntry.kind as Exclude<TabKind, "file" | "chat" | "plugin"> };
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
    openSettings("shortcuts");
  });

  // RunConfigToolbar's "Manage agents…" entry (run-config-toolbar.tsx) is
  // several component layers below the shell -- a window event rather
  // than threading a callback down through Composer/task-detail.tsx,
  // matching how app-sidebar.tsx's AccountsDialog already deep-links from
  // the Settings screen's Providers stub via `smind:open-accounts`.
  useEffect(() => {
    const onOpenSettings = (e: Event) => {
      const sectionId = (e as CustomEvent<{ sectionId?: string }>).detail?.sectionId;
      openSettings(sectionId);
    };
    window.addEventListener("smind:open-settings", onOpenSettings);
    return () => window.removeEventListener("smind:open-settings", onOpenSettings);
  }, [openSettings]);

  const { open: paletteOpen, setOpen: setPaletteOpen } = usePalette();
  useActionHandler("palette.open", () => setPaletteOpen(!paletteOpen));

  useActionHandler("app.quit", () => {
    desktop.quit().catch(() => {});
  });

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
    // Already open: keep the user's current section rather than resetting it.
    if (activeView !== "settings") openSettings();
  });

  // Plan usage opens as a modal over whatever is showing, Settings included
  // (AC32/AC67/AC68); the footer item and chat hover card claim the same
  // action later, so they never need to know about the modal.
  useActionHandler("usage.open", openUsageModal);

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
  // ZCode's left zone: the toggle and update badge ride in the sidebar's top
  // strip while it is expanded; collapsed to the rail or a compact sheet there
  // is no room, so the main header hosts them instead (exactly one of the two).
  // Desktop (non-compact): the toggle is one window-level overlay at a fixed x
  // (WindowSidebarToggle), so it neither jumps nor remounts when the sidebar
  // collapses; neither the strip nor the header hosts it then.
  const { fullscreen } = useWindowState();
  const windowToggle = isDesktop && !isMobile;
  // macOS, whenever the sidebar is not settled-expanded (collapsed rail and
  // every frame of the collapse/expand animation): the lights would straddle the
  // rail / content-panel seam, so the title row is ONE panel-coloured bar across
  // the window and the panel's corner and left border start below it.
  const lightsBar = windowToggle && !(sidebarOpen && layoutOpen) && hasTrafficLights(desktopOS, fullscreen);
  const toggleInSidebar = !isMobile && sidebarOpen && !windowToggle;
  const sidebarElement = (
    <AppSidebar
      windowToggle={windowToggle}
      client={client}
      selectedTaskId={selectedTask?.ID ?? null}
      onSelectTask={selectTaskFromSidebar}
      attention={attention}
      runStatus={runStatus}
      rateLimit={rateLimit}
      unread={unread}
      onMarkUnread={markUnread}
      events={events}
      onTasksChange={setAllTasks}
      onWorkspacesChange={(workspaces) => {
        setAllWorkspaces(workspaces);
        setTreeLoaded(true);
      }}
      onOpenSettings={openSettings}
      onOpenInbox={openInbox}
      onOpenPluginPanel={openPluginPage}
      connectionStatus={connectionStatus}
      connectionStatusText={connectError ? `Disconnected: ${connectError}` : STATUS_LABEL[connectionStatus]}
      headerActions={
        toggleInSidebar ? (
          <>
            <UpdateBadge state={appUpdate.state} onOpen={appUpdate.reopen} />
            <SidebarTrigger className="text-foreground-muted hover:text-foreground" />
          </>
        ) : windowToggle && sidebarOpen ? (
          <UpdateBadge state={appUpdate.state} onOpen={appUpdate.reopen} />
        ) : undefined
      }
    />
  );

  const headerElement = (
    <>
      <AppHeader
        statusText={connectError ? `Disconnected: ${connectError}` : STATUS_LABEL[connectionStatus]}
        connectionStatus={connectionStatus}
        sidebarPx={isMobile ? 0 : sidebarOpen ? sidebarWidth : SIDEBAR_ICON_WIDTH}
        showSidebarToggle={!toggleInSidebar && !windowToggle}
        showUpdateBadge={isMobile || !sidebarOpen}
        windowToggle={windowToggle}
        task={selectedTask}
        client={client}
        events={events}
        onOpenTerminal={focusOrOpenTerminal}
        updateState={appUpdate.state}
        onOpenUpdate={appUpdate.reopen}
      />
      {isDesktop && (
        <DesktopDaemonBanner
          client={client}
          runsLoaded={runsLoaded}
          runningRuns={[...runningChatsByTask.values()].reduce((n, chats) => n + chats.size, 0)}
        />
      )}
      {isDesktop && <DesktopUpdateDialogView update={appUpdate} />}
    </>
  );

  const emptyStateElement = (
    <div data-testid="app-empty-state" className="h-full bg-panel">
      <HomeLanding
        client={client}
        connected={client !== null && connectionStatus === "connected"}
        workspaces={allWorkspaces}
        onCreated={selectTaskFromSidebar}
        onAddProject={requestNewWorkspace}
      />
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
      onJumpToTask={jumpToTask}
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
      onOpenPluginPanel={openPluginPanel}
      chats={chats}
      openChatIds={openChatIds}
      runningChatsByTask={runningChatsByTask}
      onNewChat={openNewChat}
      onOpenChat={openChatById}
      onArchiveChat={archiveChatTab}
      showMoveAffordance={false}
      newTabMenuOpen={openNewTabPaneId === DEFAULT_PANE_ID}
      onNewTabMenuOpenChange={(open) => setOpenNewTabPaneId(open ? DEFAULT_PANE_ID : null)}
    />
  );

  const mainContentElement = (
    // bg-background is explicit (not just inherited from SidebarInset): on macOS the
    // body is transparent over the window's vibrancy, and only the sidebar may show it.
    <div data-testid="app-main-content" className={cn("min-h-0 flex-1 bg-panel", lightsBar && "border-l")}>
      {isDesktop && connectionStatus === "disconnected" ? (
        // AC6: replaces offline.html's job for the bundled flow -- shown
        // only for the "initial connect never succeeded" case (see
        // ConnectionStatus's own comment in reconnect.ts: an established
        // connection dropping goes through the automatic reconnect loop
        // and its own header status text instead, never this).
        <DesktopUnreachable
          onSwitchConnection={() => openSettings("connections")}
        />
      ) : selectedTask && taskState ? (
        // Several panes are inset frames: p-1 (4px) gives them the same gap
        // against the panel edges that ResizableHandle's own 4px width
        // already gives adjoining panes (zcode-visual-parity plan P2). A lone
        // pane is frameless -- the content panel around it is its frame.
        <div className={cn("h-full", paneCount > 1 && "p-1")}>
          {isMobile ? (
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
                onOpenPluginPanel={openPluginPanel}
                chats={chats}
                openChatIds={openChatIds}
                runningChatsByTask={runningChatsByTask}
                onNewChat={openNewChat}
                onOpenChat={openChatById}
                onJumpToTask={jumpToTask}
                onArchiveChat={archiveChatTab}
                dragOverPaneId={dragOverPaneId}
                dropPosition={dropPosition}
                isDragActive={isDragActive}
                focusedPaneId={taskState.focusedPaneId}
                onFocusPane={(paneId) => focusPane(selectedTask.ID, paneId)}
                openNewTabPaneId={openNewTabPaneId}
                onOpenNewTabPaneIdChange={setOpenNewTabPaneId}
              />
            </DndContext>
          )}
        </div>
      ) : (
        emptyStateElement
      )}
    </div>
  );

  // The inset content panel (ZCode): header + panes on the lighter panel
  // surface with a rounded top-left corner, over the sidebar-coloured window.
  // Compact has no sidebar beside it to inset from, so it stays square.
  const contentPanelElement = (
    <div
      data-testid="app-content-panel"
      className={cn(
        "flex min-h-0 flex-1 flex-col overflow-hidden bg-panel",
        // Under the lights the corner and the left border would cut through them: the bar and the rail seam draw the separation below the bar instead (the transparent top border keeps the header on the same line as when expanded).
        !isMobile && (lightsBar ? "border-t border-t-transparent" : SHELL_FRAME_PANEL_CLASS),
      )}
    >
      {headerElement}
      {mainContentElement}
    </div>
  );

  const settingsOpen = activeView === "settings";
  // Settings and the inbox both cover the shell: it is hidden and inert behind either.
  const shellCovered = activeView !== "workspace";
  // Stable identities: ShellCommands memoizes its palette entries on them.
  const selectTaskFromPalette = useCallback(
    (task: Task) => {
      closeSettings();
      selectTask(task);
    },
    [closeSettings, selectTask],
  );
  const openTabFromPalette = useCallback(
    (taskId: number, entry: TabEntry, placement?: TabPlacement) => {
      closeSettings();
      openTab(taskId, entry, placement);
    },
    [closeSettings, openTab],
  );
  const activateFromPalette = useCallback(
    (taskId: number, key: string) => {
      closeSettings();
      activate(taskId, key);
    },
    [closeSettings, activate],
  );
  // A plugin panel chosen in the palette leaves whatever covered the shell
  // first, then goes through the one shared open path (page vs task tab).
  const openPluginPanelFromPalette = useCallback(
    (panel: PluginPanel) => {
      closeSettings();
      openPluginPanel(panel);
    },
    [closeSettings, openPluginPanel],
  );
  const shellElement = (
    <SidebarProvider
      className="h-svh"
      style={{ "--sidebar-width": `${sidebarWidth}px` } as CSSProperties}
      open={layoutOpen}
      // Only ever called by the toggle: flip the *requested* state, which can
      // already differ from the (trailing) layout state.
      onOpenChange={() => setSidebarOpen((o) => !o)}
    >
      {/*
       * `sidebar.toggle` is claimed by a child of SidebarProvider rather
       * than by AppShell, because `useSidebar()` throws outside it. A
       * zero-render component is the cheapest way to be inside a provider
       * its own parent mounts.
       */}
      <SidebarToggleAction settingsOpen={shellCovered} closeMobileRef={closeMobileSidebarRef} />
      <CommandPalette />
      {/* G9: a task with a PR gets its PR tab once, in the background (never again after a close). */}
      <PRTabAutoOpen taskId={selectedTask?.ID ?? null} tabs={taskState ? collectAllTabs(taskState.root) : null} openTab={openTab} />
      {/* ADR-0042: a plugin tab whose panel left the daemon's list is closed, across every task's layout. */}
      <PluginTabReaper tabsByTask={tabsByTask} closeTab={closeTab} />
      <ShellCommands
        client={client}
        tasks={allTasks}
        workspaces={allWorkspaces}
        selectedTask={selectedTask}
        tabs={taskState ? collectAllTabs(taskState.root) : null}
        chats={chats}
        // The palette stays reachable over Settings; anything that changes
        // the selection leaves Settings first so it is not applied behind it.
        onSelectTask={selectTaskFromPalette}
        onOpenTab={openTabFromPalette}
        onActivateTab={activateFromPalette}
        onNewChat={() => {
          closeSettings();
          openNewChat("prefer");
        }}
        onOpenChat={(chatId) => {
          closeSettings();
          openChatById(chatId, "prefer");
        }}
        onOpenPluginPanel={openPluginPanelFromPalette}
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
          <SidebarInset className="min-h-0 flex-1">{contentPanelElement}</SidebarInset>
        </div>
      ) : (
        <div className="relative flex h-svh w-full">
          {windowToggle && <WindowSidebarToggle />}
          {/*
           * The sidebar-vs-content split: a plain flex row (ZCode's model),
           * not react-resizable-panels. The panel clips the shadcn Sidebar
           * (static width inside) and animates its own width between
           * `--sidebar-width` and `--sidebar-width-icon`; `data-resizing`
           * (set by useSidebarResize during a drag) turns that transition off.
           */}
          <div
            ref={sidebarPanelRef}
            data-slot="sidebar-panel"
            data-collapsed={sidebarOpen ? undefined : ""}
            className="relative h-full min-w-0 flex-none overflow-hidden bg-sidebar [&_[data-slot=sidebar-container]]:border-r-0 w-[min(var(--sidebar-width),50%)] transition-[width] duration-200 ease-out data-[collapsed]:w-(--sidebar-width-icon) data-[resizing=true]:transition-none motion-reduce:transition-none"
          >
            <div
              data-testid="sidebar-content"
              data-settling={sidebarSettling ? "" : undefined}
              inert={sidebarSettling}
              // Hidden at once while the width animates (no clipped/shifted rows),
              // then the settled layout fades in.
              className={cn("h-full", sidebarSettling ? "opacity-0 transition-none" : "opacity-100 transition-opacity duration-150 motion-reduce:transition-none")}
            >
              {sidebarElement}
            </div>
            {lightsBar && (
              <div
                aria-hidden
                data-testid="sidebar-rail-bar"
                className="pointer-events-none absolute inset-x-0 top-0 z-10 h-[49px] border-b border-border/50 bg-panel"
              />
            )}
          </div>
          {/* Only once settled-expanded: its 4px would otherwise show as a sidebar-coloured band in the title strip beside the lights while the width animates. */}
          {sidebarOpen && layoutOpen && (
            <SidebarResizeHandle
              // Zero layout width: the 4px hit area straddles the seam, so the
              // panel's own border-l is the only divider (no gutter between
              // the sidebar and the inset panel).
              className="-mx-0.5"
              data-testid="sidebar-resize-handle"
              aria-label="Resize sidebar"
              aria-valuenow={sidebarWidth}
              aria-valuemin={SIDEBAR_MIN_WIDTH}
              aria-valuemax={SIDEBAR_MAX_WIDTH}
              {...sidebarResize}
            />
          )}
          <SidebarInset className={cn("h-full min-w-0 flex-1 bg-sidebar", !isMobile && !lightsBar && SHELL_FRAME_INSET_CLASS)}>
            {contentPanelElement}
          </SidebarInset>
        </div>
      )}
      <QuickOpen
        client={client}
        task={selectedTask}
        open={quickOpenOpen}
        onOpenChange={setQuickOpenOpen}
        onOpenFile={openFileTab}
        events={events}
      />
      <ArchiveChatDialog
        client={client}
        chat={archivingChat}
        open={archivingChat !== null}
        onOpenChange={(open) => {
          if (!open) setArchivingChat(null);
        }}
        onArchived={onChatArchived}
      />
    </SidebarProvider>
  );

  // Settings is a full-screen screen with its own layout, not a pane: while
  // it is open it covers the whole shell. The shell is hidden, never
  // unmounted -- the task pane keeps its live chat stream, terminals, scroll
  // and split sizes. `invisible` (visibility:hidden) rather than `hidden`
  // (display:none) keeps the shell's box, so react-resizable-panels never
  // sees a 0-size group and re-snaps the sidebar; `inert` takes it out of
  // focus order and pointer events. Radix portals (palette, dialogs) are
  // outside this wrapper and still render on top.
  return (
    <DaemonQueryProvider queryClient={queryClient} client={client} events={events}>
      <PluginActionsBridge openPanel={openPluginPanel}>
        <div className="relative h-svh w-full">
        <PluginThemeApplier />
        <UsageModalRoot />
        <div
          data-testid="app-shell"
          className={cn("h-full w-full", shellCovered && "invisible")}
          inert={shellCovered}
          aria-hidden={shellCovered || undefined}
        >
          {shellElement}
        </div>
        {activeView === "inbox" && (
          <div className="absolute inset-0 z-40">
            <InboxScreen
              onNavigateBack={closeSettings}
              onOpenSettings={(id) => openSettings(id)}
              onOpenTask={(taskId) => {
                closeSettings();
                void jumpToTask(taskId);
              }}
            />
          </div>
        )}
        {activeView === "plugin" && pluginPage && (
          // ADR-0042: a plugin panel as a full page -- the surface for
          // sidebar panels and any panel opened with no task selected.
          <div className="absolute inset-0 z-40">
            <PluginPage
              pluginId={pluginPage.pluginId}
              panelId={pluginPage.panelId}
              onNavigateBack={closeSettings}
            />
          </div>
        )}
        {settingsOpen && (
          // No background here: on macOS the settings sidebar is a vibrancy
          // surface and the screen paints its own opaque panes.
          <div className="absolute inset-0 z-40">
            <SettingsScreen
              key={settingsOpenNonce}
              client={client}
              events={events}
              onNavigateBack={closeSettings}
              initialSectionId={settingsInitialSectionId ?? undefined}
            />
          </div>
        )}
        </div>
      </PluginActionsBridge>
    </DaemonQueryProvider>
  );
}

/**
 * What a plugin panel's view may ask the shell to do (ADR-0042): open
 * another panel, resolved through the same shared open path as every other
 * surface. An id pair the daemon no longer lists (unknown or disabled) is a
 * no-op, never an error surface. A separate component (not AppShell's own
 * body) because the panels live in the query cache, which only exists below
 * the DaemonQueryProvider AppShell itself renders.
 */
function PluginActionsBridge({ openPanel, children }: { openPanel: (panel: PluginPanel) => void; children: ReactNode }) {
  const panels = usePluginPanels();
  const actions = useMemo<PluginActions>(
    () => ({
      openPanel: (pluginId, panelId) => {
        const panel = findPanel(panels.panels, pluginId, panelId);
        if (!panel) return;
        openPanel(panel);
      },
    }),
    [panels.panels, openPanel],
  );
  return <PluginActionsProvider value={actions}>{children}</PluginActionsProvider>;
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
  chats,
  onSelectTask,
  onOpenTab,
  onActivateTab,
  onNewChat,
  onOpenChat,
  onOpenPluginPanel,
}: {
  client: WsClient | null;
  tasks: Task[];
  workspaces: Workspace[];
  selectedTask: Task | null;
  tabs: TabEntry[] | null;
  /** The selected task's chats (ADR-0016 P3), for the palette's "Open chat: <title>" entries. */
  chats: Chat[] | null;
  onSelectTask: (task: Task) => void;
  onOpenTab: (taskId: number, entry: TabEntry, placement?: TabPlacement) => void;
  onActivateTab: (taskId: number, key: string) => void;
  onNewChat: () => void;
  onOpenChat: (chatId: number) => void;
  /** "+" / palette -> one of the active plugins' panels (ADR-0042): the shell's one shared open path (page vs task tab). */
  onOpenPluginPanel: (panel: PluginPanel) => void;
}) {
  const { preference, setPreference } = useTheme();
  const pr = useTaskGit(selectedTask?.ID ?? null)?.pr;

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
    // The PR tab only exists for a task that has a PR.
    if (pr) base.push(prTab(selectedTask.ID, pr.number));
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
  }, [selectedTask, tabs, pr, onOpenTab, onActivateTab]);
  useCommands("shell:tabs", 2, tabCommands);

  // ADR-0042: every panel of the active plugins, whatever its `where` -- the
  // palette is a global opener, and the shell's shared open path decides
  // page-vs-task-tab for each entry.
  const pluginPanels = usePluginPanels().panels;
  const pluginPanelCommands = useMemo<Command[]>(
    () =>
      pluginPanels.map((panel) => ({
        id: `plugin-panel-${panel.plugin}-${panel.id}`,
        group: "Plugins",
        title: `Open ${panel.title}`,
        subtitle: panel.plugin,
        keywords: ["plugin", panel.id, "panel"],
        run: () => onOpenPluginPanel(panel),
      })),
    [pluginPanels, onOpenPluginPanel],
  );
  useCommands("plugins:panels", 4, pluginPanelCommands);

  const chatCommands = useMemo<Command[]>(() => {
    if (!selectedTask) return [];
    const open = new Set((tabs ?? []).filter((t) => t.kind === "chat").map((t) => t.chatId));
    const reopen: Command[] = (chats ?? [])
      .filter((c) => !open.has(c.ID))
      .map((c) => ({
        id: `chat-${c.ID}`,
        group: "Chats",
        title: `Open chat: ${c.Title || "Chat"}`,
        subtitle: selectedTask.Title,
        run: () => onOpenChat(c.ID),
      }));
    return [
      {
        id: "chat-new",
        group: "Chats",
        title: "New chat",
        subtitle: selectedTask.Title,
        run: onNewChat,
      },
      ...reopen,
    ];
  }, [selectedTask, tabs, chats, onNewChat, onOpenChat]);
  useCommands("shell:chats", 2, chatCommands);

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
  useSlashPaletteCommands();

  const actionCommands = useMemo<Command[]>(
    () => [
      {
        id: "open-usage",
        group: "Actions",
        title: "Usage: open plan usage",
        subtitle: "Plan windows for each account",
        keywords: ["quota", "limits", "plan", "5h", "weekly", "rate limit"],
        action: "usage.open",
        icon: "usage",
        run: openUsageModal,
      },
      {
        id: "cycle-theme",
        group: "Actions",
        title: "Cycle theme",
        subtitle: `Currently ${preference}`,
        keywords: ["dark mode", "light mode", "appearance"],
        action: "theme.cycle",
        icon: "appearance",
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

/**
 * Claims `sidebar.toggle` from inside SidebarProvider (where `useSidebar()` is
 * legal) and renders nothing. While Settings covers the shell the toggle is
 * restricted away with every other shell action (see SETTINGS_SAFE_ACTIONS),
 * and a compact-layout sidebar sheet that was open when Settings opened is closed --
 * it portals out of the hidden shell and would otherwise float over Settings.
 */
function SidebarToggleAction({
  settingsOpen,
  closeMobileRef,
}: {
  settingsOpen: boolean;
  /** Filled with a closer for the compact-layout sheet, so a task pick (outside the provider) can dismiss it. */
  closeMobileRef: { current: (() => void) | null };
}) {
  const { toggleSidebar, setOpenMobile } = useSidebar();
  useActionHandler("sidebar.toggle", toggleSidebar);
  useEffect(() => {
    closeMobileRef.current = () => setOpenMobile(false);
    return () => {
      closeMobileRef.current = null;
    };
  }, [closeMobileRef, setOpenMobile]);
  useEffect(() => {
    if (settingsOpen) setOpenMobile(false);
  }, [settingsOpen, setOpenMobile]);
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
  /** "+" -> one of the active plugins' panels (ADR-0042): opens (or activates) its plugin tab. */
  onOpenPluginPanel: (panel: PluginPanel) => void;
  /** The task's own chats (ADR-0016 P3), for the "+" menu's reopen-existing-chat section -- null while chat.list is still in flight. */
  chats: Chat[] | null;
  /** Which of `chats` already have a tab open somewhere in the task's split tree -- excluded from the "+" menu's reopen section. */
  openChatIds: ReadonlySet<number>;
  /** Which chats of each task are currently running (use-task-attention.ts) -- the "Also running" chip's data source. */
  runningChatsByTask: ReadonlyMap<number, ReadonlySet<number>>;
  /** "+" -> "New chat": creates a chat and opens its tab. */
  onNewChat: (placement: TabPlacement) => void;
  /** "+" -> an existing not-open chat: opens its tab. Also the "Also running" chip's jump target and a chat tab's own rename/archive dispatch path. */
  onOpenChat: (chatId: number, placement?: TabPlacement) => void;
  /** Selects another task (the lineage strip's parent/child jumps, PR-U2). */
  onJumpToTask: (taskId: number) => void;
  /** Tab context menu's "Archive chat" (chat tabs only) -- opens the confirm dialog. */
  onArchiveChat: (entry: TabEntry) => void;
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

/** chats minus openChatIds, as the "+" menu's reopen-section shape (ADR-0016 P3) -- null chats (still loading) renders no section rather than an empty flash. */
function reopenableChats(chats: Chat[] | null, openChatIds: ReadonlySet<number>): ChatMenuEntry[] | undefined {
  if (!chats) return undefined;
  return chats.filter((c) => !openChatIds.has(c.ID)).map((c) => ({ id: c.ID, title: c.Title }));
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
        onJumpToTask={shared.onJumpToTask}
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
        onOpenPluginPanel={shared.onOpenPluginPanel}
        chats={shared.chats}
        openChatIds={shared.openChatIds}
        runningChatsByTask={shared.runningChatsByTask}
        onNewChat={shared.onNewChat}
        onOpenChat={shared.onOpenChat}
        onArchiveChat={shared.onArchiveChat}
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
          {index > 0 && <ResizableHandle />}
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
  onOpenPluginPanel,
  chats,
  openChatIds,
  runningChatsByTask,
  onNewChat,
  onOpenChat,
  onJumpToTask,
  onArchiveChat,
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
  /** "+" -> one of the active plugins' panels (ADR-0042): opens (or activates) its plugin tab. */
  onOpenPluginPanel: (panel: PluginPanel) => void;
  chats: Chat[] | null;
  openChatIds: ReadonlySet<number>;
  runningChatsByTask: ReadonlyMap<number, ReadonlySet<number>>;
  onNewChat: (placement: TabPlacement) => void;
  onOpenChat: (chatId: number, placement?: TabPlacement) => void;
  onJumpToTask: (taskId: number) => void;
  onArchiveChat: (entry: TabEntry) => void;
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
  // The "+" menu's plugin-panel section (ADR-0042): read under the daemon
  // provider like every other panel-data consumer, here where the menu
  // renders; only the task/palette surfaces belong in a task's strip.
  const pluginPanels = usePluginPanels().panels.filter((p) => p.where === "task" || p.where === "palette");
  const showDropPreview = isDragActive && dragOverPaneId === paneId && dropPosition !== null;
  // (aria-selected, not data-state: ContextMenuTrigger asChild overwrites the trigger's data-state.)
  // Keeps the active tab visible when the strip overflows (TB-AC5).
  const tabScrollerRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const active = tabScrollerRef.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    if (active && typeof active.scrollIntoView === "function") {
      active.scrollIntoView({ inline: "nearest", block: "nearest" });
    }
  }, [activeKey]);

  return (
    <div
      ref={setDroppableRef}
      // Each pane is its own bg-panel/border frame (zcode-visual-parity
      // plan P2: WorkspaceShellLayout's independent conversation/terminal/
      // side-pane frames), with the 4px ResizableHandle width providing
      // the gap between adjoining frames. The focus ring composes with
      // that border rather than replacing it: a border would shift every
      // pane's content by its width when focus moves, and only means
      // anything once there's more than one pane to tell apart.
      className={cn(
        "relative flex h-full flex-col overflow-hidden bg-panel",
        // A lone pane is frameless: its parent is already the inset panel.
        paneCount > 1 && "rounded-lg border",
        focused && paneCount > 1 && "ring-1 ring-inset ring-ring",
      )}
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
        <div className="flex h-9 shrink-0 items-center gap-1 border-b px-1">
          {tabs.length > 0 && (
            <div
              ref={tabScrollerRef}
              data-testid="tab-scroller"
              className="min-w-0 flex-initial overflow-x-auto overflow-y-hidden [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              <TabsList variant="flat" className="w-max">
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
                    onArchiveChat={onArchiveChat}
                    hasOtherClosableTabs={tabs.some((t) => t.key !== entry.key && t.closable)}
                    hasClosableTabsToLeft={tabs.slice(0, index).some((t) => t.closable)}
                    hasClosableTabsToRight={tabs.slice(index + 1).some((t) => t.closable)}
                  />
                ))}
              </TabsList>
            </div>
          )}
          <NewTabButton
            onOpen={(kind) => onOpenBase(kind, { pane: paneId })}
            chats={reopenableChats(chats, openChatIds)}
            onOpenChat={(chatId) => onOpenChat(chatId, { pane: paneId })}
            onNewChat={() => onNewChat({ pane: paneId })}
            pluginPanels={pluginPanels}
            onOpenPluginPanel={onOpenPluginPanel}
            open={newTabMenuOpen}
            onOpenChange={onNewTabMenuOpenChange}
          />
        </div>
        {tabs.length === 0 && (
          <TabsEmptyState
            onOpen={(kind) => onOpenBase(kind, { pane: paneId })}
            chats={reopenableChats(chats, openChatIds)}
            onOpenChat={(chatId) => onOpenChat(chatId, { pane: paneId })}
            onNewChat={() => onNewChat({ pane: paneId })}
          />
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
              chats={chats}
              runningChatsByTask={runningChatsByTask}
              onOpenChat={onOpenChat}
              onJumpToTask={onJumpToTask}
            />
          </TabsContent>
        ))}
      </Tabs>
      {showDropPreview && <SplitDropPreview position={dropPosition!} />}
    </div>
  );
}

/** Split/close stay mounted and focusable but only show on hover, focus within, or the active tab (needs `group/tab` on the trigger). */
const TAB_AFFORDANCE_REVEAL =
  "pointer-events-none opacity-0 group-hover/tab:pointer-events-auto group-hover/tab:opacity-100 group-focus-within/tab:pointer-events-auto group-focus-within/tab:opacity-100 group-aria-selected/tab:pointer-events-auto group-aria-selected/tab:opacity-100";

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
  onArchiveChat,
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
  onArchiveChat: (entry: TabEntry) => void;
  hasOtherClosableTabs: boolean;
  hasClosableTabsToLeft: boolean;
  hasClosableTabsToRight: boolean;
}) {
  // Rename (terminal and chat tabs -- a file/diff tab's title is derived
  // from real identity a cosmetic override would just make misleading;
  // chat swaps its title straight through to chat.rename, App.tsx's
  // renameTabForTask dispatch) swaps the trigger for a plain input in
  // the same slot rather than trying to nest one inside TabsTrigger's
  // own <button> -- same reasoning as the close "×" below being a span,
  // not a button, but an <input> genuinely can't go inside one at all.
  // TabRenameInput (its own component/file) owns the input and its
  // focus-race fix; see that file's doc comment.
  const [renaming, setRenaming] = useState(false);
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
      <TabRenameInput
        title={entry.title}
        onCommit={(value) => {
          onRenameTab(entry.key, value);
          setRenaming(false);
        }}
        onCancel={() => setRenaming(false)}
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
          className="group/tab max-w-48 gap-1.5"
          {...listeners}
        >
          <TabLabel entry={entry} />
          {/*
           * Split/close float over the tab's right edge (no reserved slot, so tabs
           * sit tight) and appear on hover, keyboard focus within, or while active.
           * Hidden they are inert (pointer-events-none) but stay mounted, and
           * focusable for keyboard users. The active tab keeps an in-flow spacer
           * for them so its title is never covered.
           */}
          <span
            aria-hidden="true"
            className={cn("hidden shrink-0 group-aria-selected/tab:block", isMovableKind(entry.kind) && showMoveAffordance ? "w-10" : "w-5")}
          />
          <span
            data-testid="workspace-tab-affordances"
            className={cn("absolute inset-y-0 right-1 flex items-center gap-0.5", TAB_AFFORDANCE_REVEAL)}
          >
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
              className={cn("flex shrink-0 items-center rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none")}
            >
              {/* aria-hidden: this span's own aria-label already names it; without
                  hiding the icon too, it would be announced a second time as
                  part of the *ancestor* TabsTrigger's accessible name. */}
              <Icons.splitColumns aria-hidden="true" className="size-3" />
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
          className={cn("flex shrink-0 items-center rounded p-0.5 text-muted-foreground hover:bg-hover hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none")}
        >
          <Icons.close aria-hidden="true" className="size-3" />
        </span>
      )}
          </span>
        </TabsTrigger>
      </ContextMenuTrigger>
      <ContextMenuContent
        data-testid="workspace-tab-context-menu"
        data-tab-key={entry.key}
        // Radix's own dismiss-time focus restoration would otherwise move
        // focus to whatever it considers "the trigger" right as this
        // menu closes -- for Rename specifically, that trigger element
        // has just been replaced by the rename <input>, so restoring
        // focus there yanks it right back off. The explicit rAF-deferred
        // focus above is what actually focuses the input; this only
        // stops Radix's own attempt from fighting it.
        onCloseAutoFocus={(e) => e.preventDefault()}
      >
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
        {(entry.kind === "terminal" || entry.kind === "chat" || (entry.kind === "file" && entry.path)) && (
          <ContextMenuSeparator />
        )}
        {(entry.kind === "terminal" || entry.kind === "chat") && (
          <ContextMenuItem onSelect={() => setRenaming(true)} data-testid="workspace-tab-menu-rename">
            <Icons.edit />
            Rename
          </ContextMenuItem>
        )}
        {entry.kind === "chat" && (
          <ContextMenuItem onSelect={() => onArchiveChat(entry)} data-testid="workspace-tab-menu-archive-chat">
            <Icons.archive />
            Archive chat
          </ContextMenuItem>
        )}
        {entry.kind === "file" && entry.path && (
          <ContextMenuItem
            onSelect={() => {
              void navigator.clipboard?.writeText(entry.path!).catch(() => {});
            }}
            data-testid="workspace-tab-menu-copy-path"
          >
            <Icons.copy />
            Copy path
          </ContextMenuItem>
        )}
        {isMovableKind(entry.kind) && showMoveAffordance && (
          <>
            <ContextMenuSeparator />
            <ContextMenuItem onSelect={() => onSplit(entry.key, "left")} data-testid="workspace-tab-menu-split-left">
              <Icons.splitColumns />
              Split left
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "right")} data-testid="workspace-tab-menu-split-right">
              <Icons.splitColumns />
              Split right
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "up")} data-testid="workspace-tab-menu-split-up">
              <Icons.splitRows />
              Split up
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => onSplit(entry.key, "down")} data-testid="workspace-tab-menu-split-down">
              <Icons.splitRows />
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
  chats,
  runningChatsByTask,
  onOpenChat,
  onJumpToTask,
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
  chats: Chat[] | null;
  runningChatsByTask: ReadonlyMap<number, ReadonlySet<number>>;
  onOpenChat: (chatId: number, placement?: TabPlacement) => void;
  onJumpToTask: (taskId: number) => void;
}) {
  // ADR-0016 P3: this tab's own chat (undefined only for a stale tab whose
  // chat.list result hasn't loaded yet, or was deleted out from under it --
  // the "chat" branch below renders nothing rather than crashing on it).
  const chat = entry.kind === "chat" ? (chats ?? []).find((c) => c.ID === entry.chatId) : undefined;
  const isDefaultChat = entry.kind === "chat" && chats !== null && chats.length > 0 && chats[0]!.ID === entry.chatId;
  const runningChatIds = runningChatsByTask.get(task.ID);
  const otherRunningChatId =
    chat && runningChatIds ? [...runningChatIds].find((id) => id !== chat.ID) : undefined;
  const otherRunningChat =
    otherRunningChatId !== undefined
      ? { id: otherRunningChatId, title: (chats ?? []).find((c) => c.ID === otherRunningChatId)?.Title || "Chat" }
      : null;

  const renderers: Record<TabKind, React.ReactNode> = {
    chat: chat ? (
      <TaskDetailPane
        client={client}
        task={task}
        chat={chat}
        isDefaultChat={isDefaultChat}
        otherRunningChat={otherRunningChat}
        onJumpToChat={(chatId) => onOpenChat(chatId)}
        connectionStatus={connectionStatus}
        events={events}
        onOpenFile={onOpenFile}
        onOpenDiffTab={onRevealInDiff}
        onJumpToTask={onJumpToTask}
      />
    ) : null,
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
    diff: <DiffViewerPane client={client} task={task} events={events} chats={chats} />,
    pr: <PRPanelPane taskId={task.ID} focused={active} />,
    plugin: <PluginPanelPane pluginId={entry.pluginId ?? ""} panelId={entry.panelId ?? ""} taskId={task.ID} focused={active} />,
    terminal: (
      <TerminalPane
        client={client}
        task={task}
        tabKey={entry.key}
        title={entry.title}
        active={active}
        connectionStatus={connectionStatus}
        onNewTerminal={onNewTerminal}
      />
    ),
  };
  return renderers[entry.kind];
}
