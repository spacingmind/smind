import { useEffect, useState, type CSSProperties } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { Separator } from "@/components/ui/separator";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fileTab, type TabEntry, type TabKind } from "@/components/tab-registry";
import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { useTaskAttention } from "@/hooks/use-task-attention";
import { useTaskTabs } from "@/hooks/use-task-tabs";
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidth } from "@/hooks/use-sidebar-width";
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
                              aria-label={`Close ${entry.title}`}
                              data-testid="workspace-tab-close"
                              data-tab-key={entry.key}
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
