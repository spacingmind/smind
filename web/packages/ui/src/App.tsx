import { useEffect, useState } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { TaskDetailPane } from "@/components/task-detail";
import { FileExplorerPane } from "@/components/file-explorer-pane";
import { DiffViewerPane } from "@/components/diff-viewer-pane";
import { TerminalPane } from "@/components/terminal-pane";
import { Separator } from "@/components/ui/separator";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
 * The selected task's pane is a tab strip over the four independently-built
 * surfaces (Chat/run timeline, Files, Diff, Terminal) -- each of those
 * components was deliberately built standalone against a shared
 * `{ client, task }` contract in its own task, specifically so this is the
 * only place that has to know about more than one of them at once. Radix's
 * default tab behavior (unmount inactive content) is intentional here, not
 * just accepted: switching away from the Terminal tab unmounts
 * TerminalPane, which detaches its live subscription without closing the
 * session -- exactly the same "switching away only detaches" contract
 * every one of these panes already implements on task switch/unmount.
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
