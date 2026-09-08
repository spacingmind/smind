import { useEffect, useState } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { FileEditorPane } from "@/components/file-editor-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { Separator } from "@/components/ui/separator";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { fileTab, type TabEntry, type TabKind } from "@/components/tab-registry";
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

  const { tabsByTask, ensureTask, openTab, closeTab, activate } = useTaskTabs();
  const attention = useTaskAttention(client, selectedTask?.ID ?? null);

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
                    <TabContent entry={entry} client={client} task={selectedTask} connectionStatus={connectionStatus} onOpenFile={openFileTab} />
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
}: {
  entry: TabEntry;
  client: WsClient | null;
  task: Task;
  connectionStatus: ConnectionStatus;
  onOpenFile: (path: string) => void;
}) {
  const renderers: Record<TabKind, React.ReactNode> = {
    task: <TaskDetailPane client={client} task={task} connectionStatus={connectionStatus} />,
    files: <FileExplorerPane client={client} task={task} onOpenFile={onOpenFile} />,
    file: <FileEditorPane client={client} task={task} path={filePathFromKey(entry)} />,
    diff: <DiffViewerPane client={client} task={task} />,
    terminal: <TerminalPane client={client} task={task} connectionStatus={connectionStatus} />,
  };
  return renderers[entry.kind];
}

/** Extracts the file path from a file tab's `${taskId}:file:${path}` key. */
function filePathFromKey(entry: TabEntry): string {
  return entry.key.slice(entry.key.indexOf(":file:") + ":file:".length);
}
