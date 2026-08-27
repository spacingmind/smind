import { useEffect, useState } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { Separator } from "@/components/ui/separator";
import { ResizablePanel, ResizablePanelGroup } from "@/components/ui/resizable";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { connectDaemon } from "@/lib/daemon";
import type { WsClient } from "@/lib/ws-client";

/**
 * The app shell: a collapsible sidebar with live workspace/task data next
 * to an (currently empty) resizable main content area. Task detail
 * views/panes/timeline/file explorer/terminal/etc. are explicitly out of
 * scope here -- see docs/plans/active/web-ui-foundation.md -- so the main
 * area is just a placeholder pane, wired into the Resizable primitive so
 * later work can add sibling panes without restructuring this shell.
 */
export function App() {
  const [client, setClient] = useState<WsClient | null>(null);
  const [connectError, setConnectError] = useState<string | null>(null);

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
      <AppSidebar client={client} />
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
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Select a task to get started.
            </div>
          </ResizablePanel>
        </ResizablePanelGroup>
      </SidebarInset>
    </SidebarProvider>
  );
}
