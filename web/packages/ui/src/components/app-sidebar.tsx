import { useEffect, useState, type ReactNode } from "react";
import { AlertCircle, ChevronRight, FolderGit2, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";
import type { Task, Workspace } from "@/lib/types";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";

interface WorkspaceWithTasks extends Workspace {
  tasks: Task[];
}

/**
 * Loads workspace.list plus, per workspace, task.list, over the given
 * WsClient (a live connection to the daemon's /ws) -- there is no
 * mock/static data path. Returns a discriminated status so the sidebar can
 * render loading/error/empty/loaded states distinctly.
 */
function useWorkspaceTree(client: WsClient | null) {
  const [workspaces, setWorkspaces] = useState<WorkspaceWithTasks[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;

    setWorkspaces(null);
    setError(null);

    (async () => {
      const list = await client.call<Workspace[]>("workspace.list");
      const withTasks = await Promise.all(
        list.map(async (ws) => ({
          ...ws,
          tasks: await client.call<Task[]>("task.list", { workspaceId: ws.ID }),
        })),
      );
      return withTasks;
    })()
      .then((withTasks) => {
        if (!cancelled) setWorkspaces(withTasks);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      cancelled = true;
    };
  }, [client]);

  return { workspaces, error };
}

export function AppSidebar({ client }: { client: WsClient | null }) {
  const { workspaces, error } = useWorkspaceTree(client);

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <span className="text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">smind</span>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Workspaces</SidebarGroupLabel>
          <SidebarGroupContent>
            <ScrollArea className="h-full">
              <SidebarMenu>
                {error && <StatusRow icon={<AlertCircle className="size-3.5" />} className="text-destructive" text={error} />}
                {!error && workspaces === null && (
                  <StatusRow icon={<Loader2 className="size-3.5 animate-spin" />} text="Loading workspaces…" />
                )}
                {!error && workspaces?.length === 0 && <StatusRow text="No workspaces yet." />}
                {workspaces?.map((ws) => (
                  <WorkspaceItem key={ws.ID} workspace={ws} />
                ))}
              </SidebarMenu>
            </ScrollArea>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}

function StatusRow({ icon, text, className }: { icon?: ReactNode; text: string; className?: string }) {
  return (
    <SidebarMenuItem>
      <div className={cn("flex items-center gap-2 px-2 py-1.5 text-xs text-muted-foreground", className)}>
        {icon}
        <span>{text}</span>
      </div>
    </SidebarMenuItem>
  );
}

function WorkspaceItem({ workspace }: { workspace: WorkspaceWithTasks }) {
  const [open, setOpen] = useState(true);

  return (
    <SidebarMenuItem>
      <SidebarMenuButton onClick={() => setOpen((o) => !o)}>
        <FolderGit2 />
        <span className="truncate">{workspace.Title || workspace.Path}</span>
        <ChevronRight className={cn("ml-auto size-4 shrink-0 transition-transform", open && "rotate-90")} />
      </SidebarMenuButton>
      {open && (
        <SidebarMenuSub>
          {workspace.tasks.length === 0 ? (
            <SidebarMenuSubItem>
              <span className="px-2 text-xs text-muted-foreground">No tasks</span>
            </SidebarMenuSubItem>
          ) : (
            workspace.tasks.map((task) => (
              <SidebarMenuSubItem key={task.ID}>
                <SidebarMenuSubButton>
                  <span className="truncate">{task.Title}</span>
                  <span className="ml-auto shrink-0 text-[10px] uppercase text-muted-foreground">{task.Status}</span>
                </SidebarMenuSubButton>
              </SidebarMenuSubItem>
            ))
          )}
        </SidebarMenuSub>
      )}
    </SidebarMenuItem>
  );
}
