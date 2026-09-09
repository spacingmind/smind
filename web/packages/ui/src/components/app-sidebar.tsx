import { useEffect, useState, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { AlertCircle, ChevronRight, FolderGit2, Layers, Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";
import type { Space, Task, TaskStatusEventPayload, Workspace } from "@/lib/types";
import type { TaskAttention } from "@/hooks/use-task-attention";
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

/** A space plus the subset of its workspace's tasks scoped to it (Task.SpaceID === Space.ID). */
interface SpaceWithTasks extends Space {
  tasks: Task[];
}

interface WorkspaceWithTree extends Workspace {
  spaces: SpaceWithTasks[];
  /** Tasks with SpaceID === null -- always present here even when spaces.length > 0, never dropped. */
  ungroupedTasks: Task[];
}

/**
 * Loads workspace.list plus, per workspace, space.list and task.list (both
 * scoped by workspaceId, fetched in parallel), over the given WsClient (a
 * live connection to the daemon's /ws) -- there is no mock/static data
 * path. task.list returns every task in the workspace regardless of space
 * (see internal/wsapi's handleTaskList), so grouping by Task.SpaceID
 * happens client-side here rather than via a per-space query. Returns a
 * discriminated status so the sidebar can render loading/error/empty/loaded
 * states distinctly.
 */
function useWorkspaceTree(client: WsClient | null) {
  const [workspaces, setWorkspaces] = useState<WorkspaceWithTree[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;

    setWorkspaces(null);
    setError(null);

    (async () => {
      const list = await client.call<Workspace[]>("workspace.list");
      const withTree = await Promise.all(
        list.map(async (ws) => {
          const [spaces, tasks] = await Promise.all([
            client.call<Space[]>("space.list", { workspaceId: ws.ID }),
            client.call<Task[]>("task.list", { workspaceId: ws.ID }),
          ]);

          const tasksBySpaceId = new Map<number, Task[]>();
          const ungroupedTasks: Task[] = [];
          for (const task of tasks) {
            if (task.SpaceID === null) {
              ungroupedTasks.push(task);
              continue;
            }
            const bucket = tasksBySpaceId.get(task.SpaceID);
            if (bucket) bucket.push(task);
            else tasksBySpaceId.set(task.SpaceID, [task]);
          }

          return {
            ...ws,
            spaces: spaces.map((sp) => ({ ...sp, tasks: tasksBySpaceId.get(sp.ID) ?? [] })),
            ungroupedTasks,
          };
        }),
      );
      return withTree;
    })()
      .then((withTree) => {
        if (!cancelled) setWorkspaces(withTree);
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

/**
 * Live task.Status patching over the fetched tree: task.status
 * notifications (ADR 0005) update an override map in-memory; the tree
 * itself is only refetched on client change (reconnect), exactly as
 * before. Overrides are cleared whenever the client changes so a
 * reconnect never shows stale statuses alongside the fresh fetch.
 */
function useStatusOverrides(client: WsClient | null, events: DaemonEvents | null): Map<number, string> {
  const [overrides, setOverrides] = useState<Map<number, string>>(new Map());

  useEffect(() => {
    setOverrides(new Map());
  }, [client]);

  useEffect(() => {
    if (!events) return;
    return events.subscribe("task.status", (payload) => {
      const p = payload as Partial<TaskStatusEventPayload>;
      const taskId = p.taskId;
      const status = p.status;
      if (typeof taskId !== "number" || typeof status !== "string") return;
      setOverrides((prev) => {
        if (prev.get(taskId) === status) return prev;
        const next = new Map(prev);
        next.set(taskId, status);
        return next;
      });
    });
  }, [events]);

  return overrides;
}

export function AppSidebar({
  client,
  selectedTaskId = null,
  onSelectTask,
  attention,
  events,
}: {
  client: WsClient | null;
  /** The currently-selected task's id, if any, so its row can render as active. */
  selectedTaskId?: number | null;
  /** Invoked with the full Task when a task row is clicked. */
  onSelectTask?: (task: Task) => void;
  /** Per-task attention badges (App.tsx's useTaskAttention) -- renders a dot on each row that has any reason. */
  attention?: TaskAttention;
  /** The app's single-subscription event stream -- drives live task.status overrides. Optional so tests/mounts without it render as before. */
  events?: DaemonEvents | null;
}) {
  const { workspaces, error } = useWorkspaceTree(client);
  const statusOverrides = useStatusOverrides(client, events ?? null);

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
                  <WorkspaceItem
                    key={ws.ID}
                    workspace={ws}
                    selectedTaskId={selectedTaskId}
                    onSelectTask={onSelectTask}
                    attention={attention}
                    statusOverrides={statusOverrides}
                  />
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

function WorkspaceItem({
  workspace,
  selectedTaskId,
  onSelectTask,
  attention,
  statusOverrides,
}: {
  workspace: WorkspaceWithTree;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
}) {
  const [open, setOpen] = useState(true);

  // A workspace with no spaces (today's common/default case, and every
  // workspace that existed before Space wiring) renders exactly as it did
  // before this change: a flat list of tasks directly under the
  // workspace, no "Ungrouped" heading noise.
  const flat = workspace.spaces.length === 0;

  return (
    <SidebarMenuItem>
      <SidebarMenuButton onClick={() => setOpen((o) => !o)}>
        <FolderGit2 />
        <span className="truncate">{workspace.Title || workspace.Path}</span>
        <ChevronRight className={cn("ml-auto size-4 shrink-0 transition-transform", open && "rotate-90")} />
      </SidebarMenuButton>
      {open && (
        <SidebarMenuSub>
          {flat ? (
            <TaskRows
              tasks={workspace.ungroupedTasks}
              selectedTaskId={selectedTaskId}
              onSelectTask={onSelectTask}
              emptyText="No tasks"
              attention={attention}
              statusOverrides={statusOverrides}
            />
          ) : (
            <>
              {workspace.spaces.map((space) => (
                <SpaceItem key={space.ID} space={space} selectedTaskId={selectedTaskId} onSelectTask={onSelectTask} attention={attention} statusOverrides={statusOverrides} />
              ))}
              {workspace.ungroupedTasks.length > 0 && (
                <SpaceLikeItem
                  title="Ungrouped"
                  tasks={workspace.ungroupedTasks}
                  selectedTaskId={selectedTaskId}
                  onSelectTask={onSelectTask}
                  attention={attention}
                  statusOverrides={statusOverrides}
                />
              )}
            </>
          )}
        </SidebarMenuSub>
      )}
    </SidebarMenuItem>
  );
}

function SpaceItem({
  space,
  selectedTaskId,
  onSelectTask,
  attention,
  statusOverrides,
}: {
  space: SpaceWithTasks;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
}) {
  return (
    <SpaceLikeItem
      title={space.Title}
      tasks={space.tasks}
      selectedTaskId={selectedTaskId}
      onSelectTask={onSelectTask}
      attention={attention}
      statusOverrides={statusOverrides}
    />
  );
}

/**
 * Renders one collapsible second-level bucket (a real Space, or the
 * synthetic "Ungrouped" bucket for tasks with SpaceID === null) nested
 * inside a workspace's SidebarMenuSub, with its own tasks nested a level
 * further inside. SidebarMenuSub/-Item/-Button are plain ul/li/a wrappers
 * (see components/ui/sidebar.tsx), so nesting a second SidebarMenuSub
 * inside a SidebarMenuSubItem here is just ordinary nested-list markup.
 */
function SpaceLikeItem({
  title,
  tasks,
  selectedTaskId,
  onSelectTask,
  attention,
  statusOverrides,
}: {
  title: string;
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
}) {
  const [open, setOpen] = useState(true);

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton onClick={() => setOpen((o) => !o)}>
        <Layers className="size-3.5" />
        <span className="truncate">{title}</span>
        <ChevronRight className={cn("ml-auto size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
      </SidebarMenuSubButton>
      {open && (
        <SidebarMenuSub>
          <TaskRows
            tasks={tasks}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No tasks"
            attention={attention}
            statusOverrides={statusOverrides}
          />
        </SidebarMenuSub>
      )}
    </SidebarMenuSubItem>
  );
}

function TaskRows({
  tasks,
  selectedTaskId,
  onSelectTask,
  emptyText,
  attention,
  statusOverrides,
}: {
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  emptyText: string;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
}) {
  if (tasks.length === 0) {
    return (
      <SidebarMenuSubItem>
        <span className="px-2 text-xs text-muted-foreground">{emptyText}</span>
      </SidebarMenuSubItem>
    );
  }

  return (
    <>
      {tasks.map((task) => {
        const reasons = attention?.get(task.ID);
        const hasAttention = reasons !== undefined && reasons.size > 0;
        return (
          <SidebarMenuSubItem key={task.ID}>
            <SidebarMenuSubButton isActive={task.ID === selectedTaskId} onClick={() => onSelectTask?.(task)}>
              <span className="truncate">{task.Title}</span>
              {hasAttention && (
                <span
                  data-testid="task-attention"
                  aria-label="task needs attention"
                  className="ml-auto size-2 shrink-0 rounded-full bg-primary"
                />
              )}
              <span className={cn("shrink-0 text-[10px] uppercase text-muted-foreground", hasAttention && "ml-auto")}>
                {statusOverrides.get(task.ID) ?? task.Status}
              </span>
            </SidebarMenuSubButton>
          </SidebarMenuSubItem>
        );
      })}
    </>
  );
}
