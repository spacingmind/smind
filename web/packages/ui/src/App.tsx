import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { CommandPalette } from "@/components/command-palette";
import { ShortcutsDialog } from "@/components/shortcuts-dialog";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { QuickOpen } from "@/components/quick-open";
import { Separator } from "@/components/ui/separator";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  TAB_KINDS,
  defaultTabsForTask,
  fileTab,
  filePathFromTabKey,
  nextTerminalTab,
  TabLabel,
  type TabEntry,
  type TabKind,
} from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { useTheme } from "@/hooks/use-theme";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useCommands, usePalette } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { isMovableKind, useTaskTabs, type PaneId, type TabPlacement } from "@/hooks/use-task-tabs";
import { useQuickOpenShortcut } from "@/hooks/use-quick-open-shortcut";
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidth } from "@/hooks/use-sidebar-width";
import { SIDE_PANE_MAX_WIDTH, SIDE_PANE_MIN_WIDTH, useSidePaneWidth } from "@/hooks/use-side-pane-width";
import { connectDaemon } from "@/lib/daemon";
import { watchForReconnect, type ConnectionStatus, type ReconnectHandle } from "@/lib/reconnect";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

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
  const [client, setClient] = useState<WsClient | null>(null);
  const [connectError, setConnectError] = useState<string | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>("connecting");
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
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
  // Item 18: Cmd/Ctrl+P opens quick-open for the selected task. A local
  // shortcut, not a global registry entry -- see useQuickOpenShortcut's
  // doc comment for why, and what Track A should do once Item 4 lands.
  const [quickOpenOpen, setQuickOpenOpen] = useState(false);

  const { tabsByTask, ensureTask, openTab, closeTab, activate, moveTab } = useTaskTabs();
  const events = useDaemonEvents(client);
  const { attention, runStatus } = useTaskAttention(client, selectedTask?.ID ?? null, events);

  // The sidebar's user-resized width (px), persisted across reloads -- see
  // the plan's Item 6. react-resizable-panels' Panel API takes numeric
  // defaultSize/minSize/maxSize as *pixels* directly (only unitless
  // strings are treated as percentages), so these pixel values pass
  // straight through with no percentage conversion needed.
  const [sidebarWidth, setSidebarWidth] = useSidebarWidth();
  // Persisted per task (Item 6's criterion) -- taskId is null before any
  // task is selected, in which case the hook just returns the default
  // and ignores writes.
  const [sidePaneWidth, setSidePaneWidth] = useSidePaneWidth(selectedTask?.ID ?? null);

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

  useEffect(() => {
    if (!pendingRoute) return;
    const match = allTasks.find((t) => t.ID === pendingRoute.taskId);
    if (match) {
      selectTask(match);
      if (pendingRoute.tab.kind === "file") {
        openTab(match.ID, fileTab(match.ID, pendingRoute.tab.path));
      } else if (pendingRoute.tab.kind !== "task") {
        activate(match.ID, `${match.ID}:${pendingRoute.tab.kind}`);
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
    const activeEntry = taskState?.primary.tabs.find((t) => t.key === taskState.primary.activeKey);
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
  }, [selectedTask, taskState?.primary.activeKey, taskState?.primary.tabs]);

  // --- Keyboard actions the shell itself performs ---------------------
  //
  // Everything below is claimed through `useActionHandler`, never through
  // a key listener: the binding table (`keyboard/shortcuts.ts`) decides
  // *which keys* reach these, and the command palette (Item 5) can invoke
  // the same handlers with no key involved. Actions that belong to a pane
  // rather than the shell -- `composer.focus`, `run.interrupt` -- are
  // deliberately absent here; the composer claims them itself.

  useActionHandler("shortcuts.help", () => setShortcutsOpen(true));

  const { open: paletteOpen, setOpen: setPaletteOpen } = usePalette();
  useActionHandler("palette.open", () => setPaletteOpen(!paletteOpen));

  const { preference, setPreference } = useTheme();
  useActionHandler("theme.cycle", () => {
    const order: ThemePreference[] = ["light", "dark", "system"];
    const next = order[(order.indexOf(preference) + 1) % order.length]!;
    setPreference(next);
  });

  // Both bindings are scoped to the primary pane -- Item 6 adds no
  // "which pane has keyboard focus" concept (Paseo's pane.focus.*
  // actions were already out of scope for Item 4's binding table), so
  // Ctrl+W/Ctrl+Alt+<digit> reach primary's tabs only. The side pane's
  // tabs stay mouse/palette-operable.
  useActionHandler(
    "tab.close",
    () => {
      if (!selectedTask || !taskState?.primary.activeKey) return;
      const active = taskState.primary.tabs.find((t) => t.key === taskState.primary.activeKey);
      // A non-closable base tab (Chat/Files/Diff/Terminal) has no close
      // affordance in the strip either -- the shortcut matches what
      // clicking would do, rather than being a second, stronger way to
      // remove a tab the UI says can't be removed.
      if (!active?.closable) return;
      closeTab(selectedTask.ID, active.key);
    },
    { enabled: Boolean(selectedTask && taskState?.primary.activeKey) },
  );

  useActionHandler(
    "tab.jump",
    (payload) => {
      if (!selectedTask || !taskState || payload === null) return;
      const entry = taskState.primary.tabs[payload.digit - 1];
      if (entry) activate(selectedTask.ID, entry.key);
    },
    { enabled: Boolean(selectedTask && taskState) },
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
  useQuickOpenShortcut(() => setQuickOpenOpen(true), selectedTask !== null);
  return (
    <SidebarProvider>
      <AppSidebar client={client} selectedTaskId={selectedTask?.ID ?? null} onSelectTask={setSelectedTask} />
      <SidebarInset>
        <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-4" />
          <span className="text-sm text-muted-foreground">
            {connectError ? `Disconnected: ${connectError}` : STATUS_LABEL[connectionStatus]}
          </span>
        </header>
        <ResizablePanelGroup orientation="horizontal" className="flex-1">
          <ResizablePanel defaultSize={100} minSize={20}>
            {selectedTask ? (
              <Tabs defaultValue="chat" className="h-full gap-0">
                <TabsList className="mx-3 mt-2 w-fit">
                  <TabsTrigger value="chat">Chat</TabsTrigger>
                  <TabsTrigger value="files">Files</TabsTrigger>
                  <TabsTrigger value="diff">Diff</TabsTrigger>
                  <TabsTrigger value="terminal">Terminal</TabsTrigger>
                </TabsList>
                <TabsContent value="chat" className="min-h-0">
                  <TaskDetailPane client={client} task={selectedTask} connectionStatus={connectionStatus} />
                </TabsContent>
                <TabsContent value="files" className="min-h-0">
                  <FileExplorerPane client={client} task={selectedTask} />
                </TabsContent>
                <TabsContent value="diff" className="min-h-0">
                  <DiffViewerPane client={client} task={selectedTask} />
                </TabsContent>
                <TabsContent value="terminal" className="min-h-0">
                  <TerminalPane client={client} task={selectedTask} connectionStatus={connectionStatus} />
                </TabsContent>
              </Tabs>
            ) : (
              <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                Select a task to get started.
              </div>
            )}
          </ResizablePanel>
        </ResizablePanelGroup>
      </SidebarInset>
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

/** The kind→renderer lookup: adding a new tab kind means adding an entry here plus a TAB_KINDS descriptor, never editing the strip's layout logic. */
/**
 * One pane's tab strip + content: title, an "Open to side"/"Move to
 * primary" affordance on movable kinds (file/diff/terminal -- Chat and
 * Files stay pinned, per Item 6), and the existing close affordance on
 * closable kinds. Shared between the primary and side panes rather than
 * written twice, parameterized by `paneId` only for the move button's
 * direction and label.
 */
function PaneTabStrip({
  paneId,
  tabs,
  activeKey,
  task,
  client,
  connectionStatus,
  events,
  onOpenFile,
  onActivate,
  onClose,
  onMove,
  onRevealInDiff,
  onNewTerminal,
}: {
  paneId: PaneId;
  tabs: TabEntry[];
  activeKey: string | null;
  task: Task;
  client: WsClient | null;
  connectionStatus: ConnectionStatus;
  events: ReturnType<typeof useDaemonEvents>;
  onOpenFile: (path: string) => void;
  onActivate: (key: string) => void;
  onClose: (key: string) => void;
  onMove: (key: string) => void;
  onRevealInDiff: () => void;
  onNewTerminal: () => void;
}) {
  return (
    <Tabs
      key={`${task.ID}:${paneId}`}
      value={activeKey ?? undefined}
      onValueChange={onActivate}
      className="h-full gap-0"
    >
      <div className="mx-3 mt-2 overflow-x-auto">
        <TabsList className="w-fit">
          {tabs.map((entry) => (
            <TabsTrigger
              key={entry.key}
              value={entry.key}
              data-testid={`workspace-tab-${entry.kind}`}
              className="max-w-48 gap-1.5"
            >
              <TabLabel entry={entry} />
              {isMovableKind(entry.kind) && (
                <span
                  role="button"
                  tabIndex={0}
                  aria-label={
                    paneId === "primary"
                      ? `Open ${entry.title} to the side`
                      : `Move ${entry.title} to the primary pane`
                  }
                  data-testid="workspace-tab-move"
                  data-tab-key={entry.key}
                  onClick={(e) => {
                    e.stopPropagation();
                    e.preventDefault();
                    onMove(entry.key);
                  }}
                  onKeyDown={(e) => {
                    if (e.key !== "Enter" && e.key !== " ") return;
                    e.stopPropagation();
                    e.preventDefault();
                    onMove(entry.key);
                  }}
                  className="rounded px-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none"
                >
                  {/* aria-hidden: this span's own aria-label already names it; without
                      hiding the glyph too, its text content leaks into the *ancestor*
                      TabsTrigger's computed accessible name ("Diff" -> "Diff⇥"). */}
                  <span aria-hidden="true">{paneId === "primary" ? "⇥" : "⇤"}</span>
                </span>
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
                  className="rounded px-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none"
                >
                  <span aria-hidden="true">×</span>
                </span>
              )}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
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
            onRevealInDiff={onRevealInDiff}
            onNewTerminal={onNewTerminal}
            events={events}
          />
        </TabsContent>
      ))}
    </Tabs>
  );
}

function TabContent({
  entry,
  client,
  task,
  active,
  connectionStatus,
  onOpenFile,
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
  onRevealInDiff: () => void;
  onNewTerminal: () => void;
  events: ReturnType<typeof useDaemonEvents>;
}) {
  const renderers: Record<TabKind, React.ReactNode> = {
    task: <TaskDetailPane client={client} task={task} connectionStatus={connectionStatus} onOpenFile={onOpenFile} />,
    files: <FileExplorerPane client={client} task={task} onOpenFile={onOpenFile} onRevealInDiff={onRevealInDiff} events={events} />,
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
