import { useEffect, useState } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { TaskDetailPane } from "@/components/task-detail";
import { Separator } from "@/components/ui/separator";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { connectDaemon } from "@/lib/daemon";
import type { Task } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * The app shell: a collapsible sidebar with live workspace/task data next
 * to a resizable main content area that shows the selected task's detail
 * pane (run timeline + prompt form -- see task-detail.tsx) once a task is
 * clicked in the sidebar, client-side only (no navigation). File
 * explorer/CodeMirror/diff viewer/terminal are still out of scope -- see
 * docs/plans/active/web-ui-task-detail.md -- so the main area stays a
 * single pane, wired into the Resizable primitive so later work can add
 * sibling panes without restructuring this shell.
 */
export function App() {
  const [client, setClient] = useState<WsClient | null>(null);
  const [connectError, setConnectError] = useState<string | null>(null);
  const [selectedTask, setSelectedTask] = useState<Task | null>(null);

  useEffect(() => {
    let cancelled = false;
    let connected: WsClient | null = null;

    connectDaemon()
      .then((c) => {
        if (cancelled) {
          c.close();
          return;
        }
        connected = c;
        setClient(c);
      })
      .catch((err: unknown) => {
        if (!cancelled) setConnectError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      cancelled = true;
      connected?.close();
    };
  }, []);

  return (
    <SidebarProvider>
      <AppSidebar client={client} selectedTaskId={selectedTask?.ID ?? null} onSelectTask={setSelectedTask} />
      <SidebarInset>
        <header className="flex h-12 shrink-0 items-center gap-2 border-b px-3">
          <SidebarTrigger />
          <Separator orientation="vertical" className="h-4" />
          <span className="text-sm text-muted-foreground">
            {connectError ? `Disconnected: ${connectError}` : client ? "Connected to daemon" : "Connecting…"}
          </span>
        </header>
        <ResizablePanelGroup orientation="horizontal" className="flex-1">
          <ResizablePanel defaultSize={100} minSize={20}>
            {selectedTask ? (
              <TaskDetailPane client={client} task={selectedTask} />
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
