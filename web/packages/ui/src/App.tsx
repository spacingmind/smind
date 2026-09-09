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
import { fileTab, type TabEntry, type TabKind } from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { useTaskTabs } from "@/hooks/use-task-tabs";
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

  const { tabsByTask, ensureTask, openTab, closeTab, activate } = useTaskTabs();
  const events = useDaemonEvents(client);
  const attention = useTaskAttention(client, selectedTask?.ID ?? null, events);

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

  function selectTask(task: Task) {
    setSelectedTask(task);
    ensureTask(task.ID);
  }

  function openFileTab(path: string) {
    if (!selectedTask) return;
    openTab(selectedTask.ID, fileTab(selectedTask.ID, path));
  }

  const taskState = selectedTask ? tabsByTask.get(selectedTask.ID) : undefined;

  return (
    <SidebarProvider>
      <AppSidebar
        client={client}
        selectedTaskId={selectedTask?.ID ?? null}
        onSelectTask={selectTask}
        attention={attention}
        events={events}
      />
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
            {selectedTask && taskState ? (
              <Tabs
                key={selectedTask.ID}
                value={taskState.activeKey ?? undefined}
                onValueChange={(key) => activate(selectedTask.ID, key)}
                className="h-full gap-0"
              >
                <TabsList className="mx-3 mt-2 w-fit">
                  {taskState.tabs.map((entry) => (
                    <TabsTrigger key={entry.key} value={entry.key} className="gap-1.5">
                      {entry.title}
                      {entry.closable && (
                        <span
                          role="button"
                          aria-label={`Close ${entry.title}`}
                          onClick={(e) => {
                            e.stopPropagation();
                            e.preventDefault();
                            closeTab(selectedTask.ID, entry.key);
                          }}
                          className="rounded px-1 text-muted-foreground hover:bg-accent hover:text-foreground"
                        >
                          ×
                        </span>
                      )}
                    </TabsTrigger>
                  ))}
                </TabsList>
                {taskState.tabs.map((entry) => (
                  <TabsContent key={entry.key} value={entry.key} className="min-h-0">
                    <TabContent entry={entry} client={client} task={selectedTask} connectionStatus={connectionStatus} onOpenFile={openFileTab} events={events} />
                  </TabsContent>
                ))}
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
