import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import {
  AlertCircle,
  Archive,
  ChevronRight,
  FolderGit2,
  Layers,
  Loader2,
  MoreHorizontal,
  Plus,
  Settings,
  Trash2,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";
import type { Space, Task, TaskStatusEventPayload, Workspace } from "@/lib/types";
import type { TaskAttention } from "@/hooks/use-task-attention";
import { useAttentionNotifications } from "@/hooks/use-attention-notifications";
import { useNotificationPermission, type NotificationPermissionState } from "@/hooks/use-notification-permission";
import { AccountsDialog } from "@/components/accounts-dialog";
import {
  ArchiveTaskDialog,
  CreateSpaceDialog,
  CreateTaskDialog,
  CreateWorkspaceDialog,
  DeleteSpaceDialog,
  DeleteWorkspaceDialog,
} from "@/components/crud-dialogs";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";

/** Stable empty fallback for AppSidebar's own `attention?: TaskAttention` (optional prop -- e.g. tests that don't wire one up) -- a literal `new Map()` inline would be a fresh reference every render, needlessly re-running useAttentionNotifications' effect. */
const EMPTY_ATTENTION: TaskAttention = new Map();

/** The notifications toggle's label/tooltip per permission state -- also its accessible name, so a screen reader (or a test's getByRole(..., { name })) can tell the states apart. */
const NOTIFICATION_LABEL: Record<NotificationPermissionState, string> = {
  default: "Enable out-of-tab notifications",
  granted: "Notifications enabled",
  denied: "Notifications blocked -- allow them in your browser's site settings",
  unsupported: "Notifications aren't supported in this browser",
};

/** A plain inline bell glyph -- not from lucide-react, so this doesn't depend on that package happening to export one under this exact name/version. Sized like any other icon here via Button's own `[&_svg:not([class*='size-'])]:size-4` rule. */
function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </svg>
  );
}

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
 *
 * `refresh` bumps an internal counter that re-runs the fetch -- there are
 * no cross-connection workspace/space/task-created events (see the crud-ui
 * plan's Decisions), so the acting client refreshes locally after each
 * successful create/archive.
 */
function useWorkspaceTree(client: WsClient | null) {
  const [workspaces, setWorkspaces] = useState<WorkspaceWithTree[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshCounter, setRefreshCounter] = useState(0);
  const refresh = useCallback(() => setRefreshCounter((c) => c + 1), []);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;

    setWorkspaces(null);
    setError(null);

    (async () => {
      // ?? [] tolerates a daemon that answers null for an empty list
      // (defense in depth; the wire contract is backend-owned).
      const list = (await client.call<Workspace[]>("workspace.list")) ?? [];
      const withTree = await Promise.all(
        list.map(async (ws) => {
          const [spaces, tasks] = await Promise.all([
            client.call<Space[]>("space.list", { workspaceId: ws.ID }).then((r) => r ?? []),
            client.call<Task[]>("task.list", { workspaceId: ws.ID }).then((r) => r ?? []),
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
  }, [client, refreshCounter]);

  return { workspaces, error, refresh };
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

/** Which create/archive dialog a sidebar row has opened, if any. */
type CrudTarget =
  | { kind: "workspace" }
  | { kind: "space"; workspace: WorkspaceWithTree }
  | { kind: "task"; workspace: WorkspaceWithTree; spaceId: number | null }
  | { kind: "archive"; task: Task }
  | { kind: "deleteWorkspace"; workspace: WorkspaceWithTree }
  | { kind: "deleteSpace"; space: SpaceWithTasks };

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
  const { workspaces, error, refresh } = useWorkspaceTree(client);
  const statusOverrides = useStatusOverrides(client, events ?? null);

  // Out-of-tab attention notifications (Item 4): every task across the
  // whole tree, flattened just far enough to label a Notification by
  // title -- this list changing (workspace/space/task fetch completing)
  // never itself fires anything; only a *new* attention reason while the
  // tab is hidden does, in the hook itself.
  const allTasks = useMemo(
    () => (workspaces ?? []).flatMap((ws) => [...ws.spaces.flatMap((sp) => sp.tasks), ...ws.ungroupedTasks]),
    [workspaces],
  );
  const { permission: notificationPermission, requestPermission: requestNotificationPermission } =
    useNotificationPermission();
  useAttentionNotifications(attention ?? EMPTY_ATTENTION, allTasks, notificationPermission);

  const [crud, setCrud] = useState<CrudTarget | null>(null);
  const [accountsOpen, setAccountsOpen] = useState(false);
  // The just-created workspace is expanded on landing; existing ones start
  // collapsed until first refresh happens (empty state -> created).
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const didInitialLoad = useRef(false);
  useEffect(() => {
    if (workspaces === null) return;
    if (!didInitialLoad.current) {
      didInitialLoad.current = true;
      setExpanded(new Set(workspaces.map((ws) => ws.ID)));
    }
  }, [workspaces]);

  const empty = !error && workspaces !== null && workspaces.length === 0;

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <span className="text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">smind</span>
          <div className="ml-auto flex items-center gap-1 group-data-[collapsible=icon]:hidden">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={NOTIFICATION_LABEL[notificationPermission]}
              title={NOTIFICATION_LABEL[notificationPermission]}
              data-testid="notifications-toggle"
              disabled={notificationPermission !== "default"}
              // Explicit user action, per Item 4's requirement -- this is
              // the only place useNotificationPermission's requestPermission
              // is ever called; nothing here runs unprompted on load.
              onClick={requestNotificationPermission}
            >
              <BellIcon />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Accounts settings"
              onClick={() => setAccountsOpen(true)}
            >
              <Settings />
            </Button>
          </div>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="justify-between">
            <span>Workspaces</span>
            {!empty && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="New workspace"
                data-testid="sidebar-new-workspace-button"
                onClick={() => setCrud({ kind: "workspace" })}
                className="mr-1"
              >
                <Plus />
              </Button>
            )}
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <ScrollArea className="h-full">
              <SidebarMenu>
                {error && <StatusRow icon={<AlertCircle className="size-3.5" />} className="text-destructive" text={error} />}
                {!error && workspaces === null && (
                  <StatusRow icon={<Loader2 className="size-3.5 animate-spin" />} text="Loading workspaces…" />
                )}
                {empty && (
                  <SidebarMenuItem>
                    <div className="flex flex-col gap-3 px-2 py-4">
                      <div className="text-xs text-muted-foreground">
                        <p className="font-medium text-foreground">Welcome to smind</p>
                        <p className="mt-1">
                          A workspace is an existing git repo. Tasks are its isolated
                          worktrees; group them into spaces if you want. Create a
                          workspace to start.
                        </p>
                      </div>
                      <Button
                        size="sm"
                        className="w-fit"
                        data-testid="sidebar-empty-new-workspace-button"
                        onClick={() => setCrud({ kind: "workspace" })}
                      >
                        <Plus /> New workspace
                      </Button>
                    </div>
                  </SidebarMenuItem>
                )}
                {workspaces?.map((ws) => (
                  <WorkspaceItem
                    key={ws.ID}
                    workspace={ws}
                    expanded={expanded.has(ws.ID)}
                    onToggleExpanded={() =>
                      setExpanded((prev) => {
                        const next = new Set(prev);
                        if (next.has(ws.ID)) next.delete(ws.ID);
                        else next.add(ws.ID);
                        return next;
                      })
                    }
                    onAddSpace={() => setCrud({ kind: "space", workspace: ws })}
                    onAddTask={(spaceId) => setCrud({ kind: "task", workspace: ws, spaceId })}
                    onArchiveTask={(task) => setCrud({ kind: "archive", task })}
                    onDeleteWorkspace={() => setCrud({ kind: "deleteWorkspace", workspace: ws })}
                    onDeleteSpace={(space) => setCrud({ kind: "deleteSpace", space })}
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

      {client && (
        <>
          <CreateWorkspaceDialog
            client={client}
            open={crud?.kind === "workspace"}
            onOpenChange={(o) => !o && setCrud(null)}
            onCreated={(ws) => {
              didInitialLoad.current = true;
              setExpanded((prev) => new Set(prev).add(ws.ID));
              refresh();
            }}
          />
          {crud?.kind === "space" && (
            <CreateSpaceDialog
              client={client}
              workspaceId={crud.workspace.ID}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onCreated={refresh}
            />
          )}
          {crud?.kind === "task" && (
            <CreateTaskDialog
              client={client}
              workspace={crud.workspace}
              spaces={crud.workspace.spaces}
              fixedSpaceId={crud.spaceId}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onCreated={(task) => {
                onSelectTask?.(task);
                refresh();
              }}
            />
          )}
          {crud?.kind === "archive" && (
            <ArchiveTaskDialog
              client={client}
              task={crud.task}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onArchived={refresh}
            />
          )}
          {crud?.kind === "deleteWorkspace" && (
            <DeleteWorkspaceDialog
              client={client}
              workspace={crud.workspace}
              spacesRemoved={crud.workspace.spaces.length}
              tasksRemoved={
                crud.workspace.spaces.reduce((sum, sp) => sum + sp.tasks.length, 0) +
                crud.workspace.ungroupedTasks.length
              }
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onDeleted={refresh}
            />
          )}
          {crud?.kind === "deleteSpace" && (
            <DeleteSpaceDialog
              client={client}
              space={crud.space}
              tasksRemoved={crud.space.tasks.length}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onDeleted={refresh}
            />
          )}
          <AccountsDialog client={client} open={accountsOpen} onOpenChange={setAccountsOpen} />
        </>
      )}
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

/** The "⋯" hover/context menu shared by workspace and space rows. */
function RowMenu({
  items,
  triggerProps,
}: {
  items: { label: string; icon: ReactNode; onSelect: () => void }[];
  /** Extra DOM attributes (data-testid, data-*-id) so callers can disambiguate this shared trigger's otherwise-identical "Row actions" label between workspace and space rows. */
  triggerProps?: Record<string, string | number>;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Row actions" className="size-5 p-0" {...triggerProps}>
          <MoreHorizontal />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="right">
        {items.map((item) => (
          <DropdownMenuItem key={item.label} onSelect={item.onSelect}>
            {item.icon}
            {item.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function WorkspaceItem({
  workspace,
  expanded,
  onToggleExpanded,
  onAddSpace,
  onAddTask,
  onArchiveTask,
  onDeleteWorkspace,
  onDeleteSpace,
  selectedTaskId,
  onSelectTask,
  attention,
  statusOverrides,
}: {
  workspace: WorkspaceWithTree;
  expanded: boolean;
  onToggleExpanded: () => void;
  onAddSpace: () => void;
  onAddTask: (spaceId: number | null) => void;
  onArchiveTask: (task: Task) => void;
  onDeleteWorkspace: () => void;
  onDeleteSpace: (space: SpaceWithTasks) => void;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
}) {
  // A workspace with no spaces (today's common/default case, and every
  // workspace that existed before Space wiring) renders exactly as it did
  // before this change: a flat list of tasks directly under the
  // workspace, no "Ungrouped" heading noise.
  const flat = workspace.spaces.length === 0;

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        onClick={onToggleExpanded}
        data-testid="sidebar-workspace-row"
        data-workspace-id={workspace.ID}
      >
        <FolderGit2 />
        <span className="min-w-0 truncate">{workspace.Title || workspace.Path}</span>
        <ChevronRight className={cn("ml-auto size-4 shrink-0 transition-transform", expanded && "rotate-90")} />
      </SidebarMenuButton>
      <SidebarMenuAction>
        <RowMenu
          items={[
            { label: "Add task", icon: <Plus />, onSelect: () => onAddTask(null) },
            { label: "Add space", icon: <Plus />, onSelect: onAddSpace },
            { label: "Delete workspace", icon: <Trash2 />, onSelect: onDeleteWorkspace },
          ]}
          triggerProps={{
            "data-testid": "sidebar-workspace-actions-trigger",
            "data-workspace-id": workspace.ID,
          }}
        />
      </SidebarMenuAction>
      {expanded && (
        <SidebarMenuSub>
          {flat ? (
            <TaskRows
              tasks={workspace.ungroupedTasks}
              selectedTaskId={selectedTaskId}
              onSelectTask={onSelectTask}
              emptyText="No tasks"
              attention={attention}
              statusOverrides={statusOverrides}
              onArchiveTask={onArchiveTask}
            />
          ) : (
            <>
              {workspace.spaces.map((space) => (
                <SpaceItem
                  key={space.ID}
                  space={space}
                  selectedTaskId={selectedTaskId}
                  onSelectTask={onSelectTask}
                  attention={attention}
                  statusOverrides={statusOverrides}
                  onAddTask={onAddTask}
                  onArchiveTask={onArchiveTask}
                  onDeleteSpace={onDeleteSpace}
                />
              ))}
              {workspace.ungroupedTasks.length > 0 && (
                <SpaceLikeItem
                  title="Ungrouped"
                  tasks={workspace.ungroupedTasks}
                  selectedTaskId={selectedTaskId}
                  onSelectTask={onSelectTask}
                  attention={attention}
                  statusOverrides={statusOverrides}
                  onArchiveTask={onArchiveTask}
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
  onAddTask,
  onArchiveTask,
  onDeleteSpace,
}: {
  space: SpaceWithTasks;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
  onAddTask: (spaceId: number | null) => void;
  onArchiveTask: (task: Task) => void;
  onDeleteSpace: (space: SpaceWithTasks) => void;
}) {
  return (
    <SpaceLikeItem
      title={space.Title}
      tasks={space.tasks}
      selectedTaskId={selectedTaskId}
      onSelectTask={onSelectTask}
      attention={attention}
      statusOverrides={statusOverrides}
      onArchiveTask={onArchiveTask}
      testId="sidebar-space-row"
      spaceId={space.ID}
    >
      <SidebarMenuSubItem className="absolute top-1 right-1 flex items-center">
        <RowMenu
          items={[
            { label: "Add task", icon: <Plus />, onSelect: () => onAddTask(space.ID) },
            { label: "Delete space", icon: <Trash2 />, onSelect: () => onDeleteSpace(space) },
          ]}
          triggerProps={{
            "data-testid": "sidebar-space-actions-trigger",
            "data-space-id": space.ID,
          }}
        />
      </SidebarMenuSubItem>
    </SpaceLikeItem>
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
  onArchiveTask,
  children,
  testId,
  spaceId,
}: {
  title: string;
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
  onArchiveTask: (task: Task) => void;
  children?: ReactNode;
  /** Only set for a real Space (not the synthetic "Ungrouped" bucket, which has no Space to key on). */
  testId?: string;
  spaceId?: number;
}) {
  const [open, setOpen] = useState(true);

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        onClick={() => setOpen((o) => !o)}
        data-testid={testId}
        data-space-id={spaceId}
      >
        <Layers className="size-3.5" />
        <span className="min-w-0 truncate">{title}</span>
        <ChevronRight className={cn("ml-auto size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
      </SidebarMenuSubButton>
      {children}
      {open && (
        <SidebarMenuSub>
          <TaskRows
            tasks={tasks}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No tasks"
            attention={attention}
            statusOverrides={statusOverrides}
            onArchiveTask={onArchiveTask}
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
  onArchiveTask,
}: {
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  emptyText: string;
  attention?: TaskAttention;
  statusOverrides: Map<number, string>;
  onArchiveTask: (task: Task) => void;
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
            <SidebarMenuSubButton
              isActive={task.ID === selectedTaskId}
              onClick={() => onSelectTask?.(task)}
              data-testid="sidebar-task-row"
              data-task-id={task.ID}
            >
              <span className="min-w-0 truncate">{task.Title}</span>
              {hasAttention && (
                <span
                  data-testid="task-attention"
                  aria-label="task needs attention"
                  className="ml-auto size-2 shrink-0 rounded-full bg-primary"
                />
              )}
              <span
                data-testid="sidebar-task-status"
                className={cn("shrink-0 text-[10px] uppercase text-muted-foreground", hasAttention && "ml-auto")}
              >
                {statusOverrides.get(task.ID) ?? task.Status}
              </span>
            </SidebarMenuSubButton>
            <span className="absolute top-0.5 right-0 opacity-0 transition-opacity group-hover/menu-sub-item:opacity-100 focus-within/menu-sub-item:opacity-100">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${task.Title}`}
                    data-testid="sidebar-task-actions-trigger"
                    data-task-id={task.ID}
                    className="size-5 p-0"
                  >
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem data-testid="sidebar-task-archive-action" onSelect={() => onArchiveTask(task)}>
                    <Archive /> Archive task
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          </SidebarMenuSubItem>
        );
      })}
    </>
  );
}
