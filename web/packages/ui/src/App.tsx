import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { CommandPalette } from "@/components/command-palette";
import { ShortcutsDialog } from "@/components/shortcuts-dialog";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { Separator } from "@/components/ui/separator";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { TAB_KINDS, defaultTabsForTask, fileTab, type TabEntry, type TabKind } from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { useTheme } from "@/hooks/use-theme";
import { KeyboardProvider, useActionHandler } from "@/keyboard/keyboard-provider";
import { PaletteProvider, useCommands, usePalette } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { useTaskTabs } from "@/hooks/use-task-tabs";
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidth } from "@/hooks/use-sidebar-width";
import { connectDaemon } from "@/lib/daemon";
import { watchForReconnect, type ConnectionStatus, type ReconnectHandle } from "@/lib/reconnect";
import type { ThemePreference } from "@/lib/theme";
import type { Task, TaskFilesResult, Workspace } from "@/lib/types";
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
  const [client, setClient] = useState<WsClient | null>(null);
  const [connectError, setConnectError] = useState<string | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>("connecting");
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  // Every task across the tree, handed up by AppSidebar (the one component
  // that already fetches it) so the shell can walk it for task.prev/next.
  const [allTasks, setAllTasks] = useState<Task[]>([]);
  const [allWorkspaces, setAllWorkspaces] = useState<Workspace[]>([]);

  const { tabsByTask, ensureTask, openTab, closeTab, activate } = useTaskTabs();
  const events = useDaemonEvents(client);
  const attention = useTaskAttention(client, selectedTask?.ID ?? null, events);

  // The sidebar's user-resized width (px), persisted across reloads -- see
  // the plan's Item 6. react-resizable-panels' Panel API takes numeric
  // defaultSize/minSize/maxSize as *pixels* directly (only unitless
  // strings are treated as percentages), so these pixel values pass
  // straight through with no percentage conversion needed.
  const [sidebarWidth, setSidebarWidth] = useSidebarWidth();

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
    openTab(selectedTask.ID, fileTab(selectedTask.ID, path));
  }

  const taskState = selectedTask ? tabsByTask.get(selectedTask.ID) : undefined;

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

  useActionHandler(
    "tab.close",
    () => {
      if (!selectedTask || !taskState?.activeKey) return;
      const active = taskState.tabs.find((t) => t.key === taskState.activeKey);
      // A non-closable base tab (Chat/Files/Diff/Terminal) has no close
      // affordance in the strip either -- the shortcut matches what
      // clicking would do, rather than being a second, stronger way to
      // remove a tab the UI says can't be removed.
      if (!active?.closable) return;
      closeTab(selectedTask.ID, active.key);
    },
    { enabled: Boolean(selectedTask && taskState?.activeKey) },
  );

  useActionHandler(
    "tab.jump",
    (payload) => {
      if (!selectedTask || !taskState || payload === null) return;
      const entry = taskState.tabs[payload.digit - 1];
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
    >
      {/*
       * `sidebar.toggle` is claimed by a child of SidebarProvider rather
       * than by AppShell, because `useSidebar()` throws outside it. A
       * zero-render component is the cheapest way to be inside a provider
       * its own parent mounts.
       */}
      <SidebarToggleAction />
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
      <CommandPalette />
      <ShellCommands
        client={client}
        tasks={allTasks}
        workspaces={allWorkspaces}
        selectedTask={selectedTask}
        tabs={taskState?.tabs ?? null}
        onSelectTask={selectTask}
        onOpenTab={openTab}
        onActivateTab={activate}
      />
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
          defaultSize={sidebarWidth}
          minSize={SIDEBAR_MIN_WIDTH}
          maxSize={SIDEBAR_MAX_WIDTH}
          onResize={(size) => setSidebarWidth(size.inPixels)}
          className="min-w-0"
        >
          <AppSidebar
            client={client}
            selectedTaskId={selectedTask?.ID ?? null}
            onSelectTask={selectTask}
            attention={attention}
            events={events}
            onTasksChange={setAllTasks}
            onWorkspacesChange={setAllWorkspaces}
          />
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
            <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
              <SidebarTrigger />
              <Separator orientation="vertical" className="h-4" />
              <span className="text-sm text-muted-foreground" data-testid="app-connection-status">
                {connectError ? `Disconnected: ${connectError}` : STATUS_LABEL[connectionStatus]}
              </span>
            </header>
            {/*
             * flex-1 (grow from a 0 basis) + min-h-0 is what the old inner
             * ResizablePanelGroup gave this area for free -- it filled all
             * height left over after the header's own (shrink-0) box,
             * rather than a percentage height fighting the header for
             * space. Restated explicitly here now that that group's gone
             * (it wrapped a single always-100% panel, which was never
             * actually resizable -- see Item 6).
             */}
            <div className="flex-1 min-h-0">
              {selectedTask && taskState ? (
                <Tabs
                  key={selectedTask.ID}
                  value={taskState.activeKey ?? undefined}
                  onValueChange={(key) => activate(selectedTask.ID, key)}
                  className="h-full gap-0"
                >
                  <div className="mx-3 mt-2 overflow-x-auto">
                    <TabsList className="w-fit">
                      {taskState.tabs.map((entry) => (
                        <TabsTrigger
                          key={entry.key}
                          value={entry.key}
                          data-testid={`workspace-tab-${entry.kind}`}
                          className="max-w-48 gap-1.5"
                        >
                          <span className="min-w-0 truncate">{entry.title}</span>
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
                                closeTab(selectedTask.ID, entry.key);
                              }}
                              // A `role="button"` element gets none of a
                              // real <button>'s key handling for free, so
                              // Enter and Space are wired explicitly
                              // (uiux-audit.md §4 P1 item 9). It stays a
                              // span rather than becoming a <button>
                              // because it sits inside Radix's
                              // TabsTrigger, which is already a button --
                              // nesting one inside another is invalid HTML
                              // and React warns about it.
                              onKeyDown={(e) => {
                                if (e.key !== "Enter" && e.key !== " ") return;
                                e.stopPropagation();
                                e.preventDefault();
                                closeTab(selectedTask.ID, entry.key);
                              }}
                              className="rounded px-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-ring/50 focus-visible:ring-2 focus-visible:outline-none"
                            >
                              ×
                            </span>
                          )}
                        </TabsTrigger>
                      ))}
                    </TabsList>
                  </div>
                  {taskState.tabs.map((entry) => (
                    <TabsContent key={entry.key} value={entry.key} className="min-h-0">
                      <TabContent entry={entry} client={client} task={selectedTask} connectionStatus={connectionStatus} onOpenFile={openFileTab} events={events} />
                    </TabsContent>
                  ))}
                </Tabs>
              ) : (
                <div
                  data-testid="app-empty-state"
                  className="flex h-full items-center justify-center text-sm text-muted-foreground"
                >
                  Select a task to get started.
                </div>
              )}
            </div>
          </SidebarInset>
        </ResizablePanel>
      </ResizablePanelGroup>
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
  onOpenTab: (taskId: number, entry: TabEntry) => void;
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
        else onOpenTab(selectedTask.ID, entry);
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
      run: () => onOpenTab(selectedTask.ID, fileTab(selectedTask.ID, path)),
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
function TabContent({
  entry,
  client,
  task,
  connectionStatus,
  onOpenFile,
  events,
}: {
  entry: TabEntry;
  client: WsClient | null;
  task: Task;
  connectionStatus: ConnectionStatus;
  onOpenFile: (path: string) => void;
  events: ReturnType<typeof useDaemonEvents>;
}) {
  const renderers: Record<TabKind, React.ReactNode> = {
    task: <TaskDetailPane client={client} task={task} connectionStatus={connectionStatus} />,
    files: <FileExplorerPane client={client} task={task} onOpenFile={onOpenFile} />,
    file: <FileEditorPane client={client} task={task} path={filePathFromKey(entry)} events={events} />,
    diff: <DiffViewerPane client={client} task={task} events={events} />,
    terminal: <TerminalPane client={client} task={task} connectionStatus={connectionStatus} />,
  };
  return renderers[entry.kind];
}

/** Extracts the file path from a file tab's `${taskId}:file:${path}` key. */
function filePathFromKey(entry: TabEntry): string {
  return entry.key.slice(entry.key.indexOf(":file:") + ":file:".length);
}
