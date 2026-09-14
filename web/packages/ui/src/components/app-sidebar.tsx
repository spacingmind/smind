import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import {
  AlertCircle,
  Archive,
  ChevronRight,
  FolderGit2,
  GitBranch,
  Layers,
  Loader2,
  MoreHorizontal,
  Plus,
  Search,
  Settings,
  SlidersHorizontal,
  Trash2,
} from "lucide-react";

import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";
import type { Space, Task, TaskStat, TaskStatusEventPayload, Workspace } from "@/lib/types";
import {
  applyLifecycleEvent,
  buildWorkspaceTree,
  LIFECYCLE_TOPICS,
  searchTasks,
  type SpaceWithTasks,
  type TaskSearchResult,
  type WorkspaceWithTree,
} from "@/lib/workspace-tree";
import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import { aggregateStatus, attentionDotStatus, primaryAttentionReason, runDotStatus, workspaceTasks } from "@/lib/sidebar-signal";
import { useTaskStats, type TaskStats } from "@/hooks/use-task-stats";
import { useAttentionNotifications } from "@/hooks/use-attention-notifications";
import { useNotificationPermission } from "@/hooks/use-notification-permission";
import { useSettingsOpen } from "@/hooks/use-settings-open";
import { AccountsDialog } from "@/components/accounts-dialog";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { ThemeToggle } from "@/components/theme-toggle";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
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
import { EmptyState } from "@/components/ui/empty-state";
import { Input } from "@/components/ui/input";
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

/** Stable empty fallback for the optional `runStatus` prop, for the same reason EMPTY_ATTENTION exists: a literal `new Map()` inline would re-run the row-signal memo every render. */
const EMPTY_RUN_STATUS: TaskRunStatus = new Map();

/** Human-readable reason text for the task row's attention dot -- part of its accessible name, so the three reasons are distinguishable to a screen reader and not only by colour. */
const ATTENTION_LABEL: Record<AttentionReason, string> = {
  error: "a run failed",
  permission: "a permission is waiting",
  finished: "a run finished",
};

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
 * ADR 0009's lifecycle topics then keep that tree live: a workspace,
 * space or task created, archived or deleted anywhere -- a second browser
 * tab, or the CLI, both of which drive this same daemon -- is spliced into
 * the tree here without a refetch. Delivery is live-only with no replay,
 * so the full fetch remains the source of truth and re-runs on the two
 * occasions an event stream cannot cover: a new connection (`client`
 * changes on every reconnect) and `event.dropped`, the daemon's synthetic
 * notification that its per-connection queue overflowed.
 *
 * `refresh` bumps an internal counter that re-runs the fetch. The acting
 * client still calls it after its own successful mutation even though the
 * event covers that case too -- events.subscribe is fire-and-forget, so a
 * connection that failed to subscribe must not silently stop showing this
 * user's own new rows. `applyLifecycleEvent` upserts by ID, so the two
 * paths converge instead of double-inserting (ADR 0009: an event is not
 * ordered against the RPC response that caused it).
 */
function useWorkspaceTree(client: WsClient | null, events: DaemonEvents | null) {
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
      return await Promise.all(
        list.map(async (ws) => {
          const [spaces, tasks] = await Promise.all([
            client.call<Space[]>("space.list", { workspaceId: ws.ID }).then((r) => r ?? []),
            client.call<Task[]>("task.list", { workspaceId: ws.ID }).then((r) => r ?? []),
          ]);
          return buildWorkspaceTree(ws, spaces, tasks);
        }),
      );
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

  useEffect(() => {
    if (!events) return;
    const offs = LIFECYCLE_TOPICS.map((topic) =>
      events.subscribe(topic, (payload) => {
        // A null tree means the initial fetch is still in flight; that
        // fetch reads committed state, so it already includes whatever
        // this event describes and splicing into nothing would be lost
        // anyway.
        setWorkspaces((prev) => (prev === null ? prev : applyLifecycleEvent(prev, topic, payload)));
      }),
    );
    // The daemon dropped events for this connection, so the tree may have
    // missed a create or a delete entirely -- ADR 0009's reconciliation
    // rule is a full refetch, the same one a reconnect performs.
    offs.push(events.subscribe("event.dropped", () => refresh()));
    return () => {
      for (const off of offs) off();
    };
  }, [events, refresh]);

  return { workspaces, error, refresh };
}

/**
 * Everything a sidebar row needs to draw its signal, in one context
 * rather than several props threaded through four levels of nesting
 * (AppSidebar -> WorkspaceItem -> SpaceItem -> SpaceLikeItem -> TaskRows).
 * A mount without live data -- a test, or a connection whose
 * events.subscribe failed -- renders the same rows with the signal slots
 * empty rather than throwing.
 */
interface RowSignal {
  attention?: TaskAttention;
  /** Live task.status overrides keyed by task id (see useStatusOverrides). */
  statusOverrides: Map<number, string>;
  /** Latest run status per task (see useTaskAttention). */
  runStatus: TaskRunStatus;
  /** Per-task branch and diff size (see useTaskStats). */
  stats: TaskStats;
}

const EMPTY_SIGNAL: RowSignal = { statusOverrides: new Map(), runStatus: new Map(), stats: new Map() };

const RowSignalContext = createContext<RowSignal>(EMPTY_SIGNAL);

function useRowSignal(): RowSignal {
  return useContext(RowSignalContext);
}

/**
 * Live task.Status patching over the fetched tree: task.status
 * notifications (ADR 0005) update an override map in-memory; the tree
 * itself is only refetched on client change (reconnect) or event.dropped,
 * exactly as before. Overrides are cleared on either occasion so a
 * reconnect, or a resync after a dropped event, never shows a stale
 * override shadowing the freshly-fetched task.Status underneath it --
 * ADR 0005's queue is per-connection, not per-topic, so a drop can just as
 * well have swallowed a task.status this map would otherwise never
 * correct.
 */
function useStatusOverrides(client: WsClient | null, events: DaemonEvents | null): Map<number, string> {
  const [overrides, setOverrides] = useState<Map<number, string>>(new Map());

  useEffect(() => {
    setOverrides(new Map());
  }, [client]);

  useEffect(() => {
    if (!events) return;
    const offStatus = events.subscribe("task.status", (payload) => {
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
    const offDropped = events.subscribe("event.dropped", () => setOverrides(new Map()));
    return () => {
      offStatus();
      offDropped();
    };
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
  runStatus,
  events,
}: {
  client: WsClient | null;
  /** The currently-selected task's id, if any, so its row can render as active. */
  selectedTaskId?: number | null;
  /** Invoked with the full Task when a task row is clicked. */
  onSelectTask?: (task: Task) => void;
  /** Per-task attention badges (App.tsx's useTaskAttention) -- renders a dot on each row that has any reason. */
  attention?: TaskAttention;
  /** Per-task latest-run status (App.tsx's useTaskAttention, same hook) -- renders the row's leading run dot and feeds the container rows' aggregate. */
  runStatus?: TaskRunStatus;
  /** The app's single-subscription event stream -- drives live task.status overrides. Optional so tests/mounts without it render as before. */
  events?: DaemonEvents | null;
}) {
  const { workspaces, error, refresh } = useWorkspaceTree(client, events ?? null);
  const statusOverrides = useStatusOverrides(client, events ?? null);
  const workspaceIds = useMemo(() => (workspaces ?? []).map((ws) => ws.ID), [workspaces]);
  const stats = useTaskStats(client, events ?? null, workspaceIds);
  const rowSignal = useMemo<RowSignal>(
    () => ({ attention, statusOverrides, runStatus: runStatus ?? EMPTY_RUN_STATUS, stats }),
    [attention, statusOverrides, runStatus, stats],
  );

  // Out-of-tab attention notifications (Item 4): every task across the
  // whole tree, flattened just far enough to label a Notification by
  // title -- this list changing (workspace/space/task fetch completing)
  // never itself fires anything; only a *new* attention reason while the
  // tab is hidden does, in the hook itself.
  const allTasks = useMemo(
    () => (workspaces ?? []).flatMap((ws) => [...ws.spaces.flatMap((sp) => sp.tasks), ...ws.ungroupedTasks]),
    [workspaces],
  );
  const { permission: notificationPermission } = useNotificationPermission();
  useAttentionNotifications(attention ?? EMPTY_ATTENTION, allTasks, notificationPermission);

  const [crud, setCrud] = useState<CrudTarget | null>(null);
  const [accountsOpen, setAccountsOpen] = useState(false);
  const { open: settingsOpen, setOpen: setSettingsOpen, openSettings } = useSettingsOpen();
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

  // Collapsed search (audit-deepseek-harness.md §3): a header action that
  // expands into a field. An outside click collapses it only while the
  // query is empty -- a typed query survives clicking away, because
  // dismissing someone's search by accident is worse than leaving a field
  // open. The tree's `expanded` set is never touched while searching, so
  // clearing the query restores exactly the expansion state it had.
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLDivElement | null>(null);
  const searching = query.trim() !== "";

  useEffect(() => {
    if (!searchOpen) return;
    const onPointerDown = (e: MouseEvent) => {
      if (searchRef.current?.contains(e.target as Node)) return;
      setQuery((current) => {
        if (current.trim() === "") setSearchOpen(false);
        return current;
      });
    };
    document.addEventListener("mousedown", onPointerDown);
    return () => document.removeEventListener("mousedown", onPointerDown);
  }, [searchOpen]);

  const results = useMemo(() => searchTasks(workspaces ?? [], query), [workspaces, query]);

  const empty = !error && workspaces !== null && workspaces.length === 0;

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <span className="text-sm font-semibold tracking-tight group-data-[collapsible=icon]:hidden">smind</span>
          <div className="ml-auto flex items-center gap-1 group-data-[collapsible=icon]:hidden">
            <ThemeToggle />
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Accounts settings"
              onClick={() => setAccountsOpen(true)}
            >
              <Settings />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Settings"
              data-testid="sidebar-settings-button"
              onClick={openSettings}
            >
              <SlidersHorizontal />
            </Button>
          </div>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel className="justify-between" ref={searchRef}>
            {searchOpen ? (
              <Input
                autoFocus
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key !== "Escape") return;
                  // Escape clears a query first and only closes an
                  // already-empty field, so one keystroke never loses
                  // both the search and what was typed into it.
                  if (query === "") setSearchOpen(false);
                  else setQuery("");
                }}
                placeholder="Search tasks"
                aria-label="Search tasks"
                data-testid="sidebar-search-input"
                className="h-6 rounded-md px-1.5 text-xs"
              />
            ) : (
              <>
                <span>Workspaces</span>
                <span className="flex items-center">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Search tasks"
                    data-testid="sidebar-search-toggle"
                    onClick={() => setSearchOpen(true)}
                  >
                    <Search />
                  </Button>
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
                </span>
              </>
            )}
          </SidebarGroupLabel>
          <SidebarGroupContent>
            <ScrollArea className="h-full">
              <RowSignalContext.Provider value={rowSignal}>
                <SidebarMenu>
                  {error && <StatusRow icon={<AlertCircle className="size-3.5" />} className="text-destructive" text={error} />}
                  {!error && workspaces === null && (
                    <StatusRow icon={<Loader2 className="size-3.5 animate-spin" />} text="Loading workspaces…" />
                  )}
                  {empty && !searching && (
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
                  {searching && (
                    <SearchResults
                      results={results}
                      query={query}
                      selectedTaskId={selectedTaskId}
                      onSelectTask={onSelectTask}
                      onArchiveTask={(task) => setCrud({ kind: "archive", task })}
                    />
                  )}
                  {!searching &&
                    workspaces?.map((ws) => (
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
                    />
                  ))}
                </SidebarMenu>
              </RowSignalContext.Provider>
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
          <SettingsScreen client={client} open={settingsOpen} onOpenChange={setSettingsOpen} />
        </>
      )}
    </Sidebar>
  );
}

/**
 * The flat list a non-blank query replaces the tree with
 * (audit-deepseek-harness.md §3). Rows are the same TaskRows the tree
 * renders -- same dots, same meta line, same actions menu -- so a task
 * found by search behaves identically to one found by browsing; only the
 * grouping is gone.
 */
function SearchResults({
  results,
  query,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
}: {
  results: TaskSearchResult[];
  query: string;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
}) {
  if (results.length === 0) {
    return (
      <SidebarMenuItem>
        <EmptyState
          testId="sidebar-search-empty"
          title="No matching tasks"
          description={`Nothing matches “${query.trim()}”`}
          className="py-6"
        />
      </SidebarMenuItem>
    );
  }

  return (
    <SidebarMenuItem data-testid="sidebar-search-results">
      <SidebarMenuSub>
        <TaskRows
          tasks={results.map((r) => r.task)}
          selectedTaskId={selectedTaskId}
          onSelectTask={onSelectTask}
          emptyText="No tasks"
          onArchiveTask={onArchiveTask}
        />
      </SidebarMenuSub>
    </SidebarMenuItem>
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
}) {
  // A workspace with no spaces (today's common/default case, and every
  // workspace that existed before Space wiring) renders exactly as it did
  // before this change: a flat list of tasks directly under the
  // workspace, no "Ungrouped" heading noise.
  const flat = workspace.spaces.length === 0;
  const { attention, runStatus } = useRowSignal();
  const aggregate = aggregateStatus(workspaceTasks(workspace), attention, runStatus);

  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        onClick={onToggleExpanded}
        data-testid="sidebar-workspace-row"
        data-workspace-id={workspace.ID}
      >
        <FolderGit2 />
        <span className="min-w-0 truncate">{workspace.Title || workspace.Path}</span>
        <AggregateSlot status={aggregate} label={`${workspace.Title || workspace.Path} activity`} />
        <ChevronRight className={cn("size-4 shrink-0 transition-transform", expanded && "rotate-90")} />
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
  onAddTask,
  onArchiveTask,
  onDeleteSpace,
}: {
  space: SpaceWithTasks;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
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
  onArchiveTask,
  children,
  testId,
  spaceId,
}: {
  title: string;
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
  children?: ReactNode;
  /** Only set for a real Space (not the synthetic "Ungrouped" bucket, which has no Space to key on). */
  testId?: string;
  spaceId?: number;
}) {
  const [open, setOpen] = useState(true);
  const { attention, runStatus } = useRowSignal();
  const aggregate = aggregateStatus(tasks, attention, runStatus);

  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton
        onClick={() => setOpen((o) => !o)}
        data-testid={testId}
        data-space-id={spaceId}
      >
        <Layers className="size-3.5" />
        <span className="min-w-0 truncate">{title}</span>
        <AggregateSlot status={aggregate} label={`${title} activity`} />
        <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
      </SidebarMenuSubButton>
      {children}
      {open && (
        <SidebarMenuSub>
          <TaskRows
            tasks={tasks}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No tasks"
            onArchiveTask={onArchiveTask}
          />
        </SidebarMenuSub>
      )}
    </SidebarMenuSubItem>
  );
}

/**
 * The aggregate-activity dot a workspace or space row carries for the
 * tasks beneath it (audit-paseo.md §5's workspace status bucket), so a
 * collapsed container still says whether anything below it needs the
 * user. Always in the DOM at a fixed width, dot or no dot, so a container
 * row never reflows when something below it starts or finishes -- the
 * same reserved-slot rule the task row's attention dot follows (Item 2's
 * layout stability; refs/paseo/docs/design.md §11).
 */
function AggregateSlot({ status, label }: { status: StatusDotStatus | null; label: string }) {
  return (
    <span data-testid="row-aggregate-slot" className="ml-auto flex w-2.5 shrink-0 items-center justify-center">
      {status && <StatusDot status={status} data-testid="row-aggregate" data-aggregate={status} aria-label={label} />}
    </span>
  );
}

/**
 * The task row's second line: its branch and the size of its diff, the two
 * things `audit-paseo.md` §5's sidebar meta row leads with.
 *
 * Always rendered, even with nothing to say, so the row's height is the
 * same for every task at every moment -- Item 2's layout-stability rule
 * applied to the one thing a reserved-width slot cannot fix. A task with
 * no stat (never run, no worktree, or a stat the daemon could not compute)
 * shows its coarse lifecycle status instead of a fabricated "0 files",
 * which would read as "no changes" and be a different, wrong claim.
 */
function TaskMetaRow({ stat, status }: { stat?: TaskStat; status: string }) {
  return (
    <span
      data-testid="sidebar-task-meta"
      className="flex h-4 w-full items-center gap-1.5 overflow-hidden text-[10px] text-muted-foreground"
    >
      {stat ? (
        <>
          <GitBranch className="size-2.5 shrink-0" />
          <span data-testid="sidebar-task-branch" className="min-w-0 truncate" title={stat.branch}>
            {stat.branch}
          </span>
          {stat.filesChanged > 0 && (
            <span
              data-testid="sidebar-task-diffstat"
              className="ml-auto shrink-0 tabular-nums"
              title={`${stat.filesChanged} changed, +${stat.insertions} -${stat.deletions}`}
            >
              {stat.filesChanged}f <span className="text-status-success">+{stat.insertions}</span>{" "}
              <span className="text-status-danger">-{stat.deletions}</span>
            </span>
          )}
        </>
      ) : (
        <span data-testid="sidebar-task-status" className="uppercase">
          {status}
        </span>
      )}
    </span>
  );
}

function TaskRows({
  tasks,
  selectedTaskId,
  onSelectTask,
  emptyText,
  onArchiveTask,
}: {
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  emptyText: string;
  onArchiveTask: (task: Task) => void;
}) {
  const { attention, statusOverrides, runStatus, stats } = useRowSignal();

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
        const reason = primaryAttentionReason(attention?.get(task.ID));
        const runState = runStatus.get(task.ID);
        const runDot = runDotStatus(runState);
        const stat = stats.get(task.ID);
        return (
          <SidebarMenuSubItem key={task.ID}>
            <SidebarMenuSubButton
              className="h-auto flex-col items-stretch gap-0.5 py-1"
              isActive={task.ID === selectedTaskId}
              onClick={() => onSelectTask?.(task)}
              data-testid="sidebar-task-row"
              data-task-id={task.ID}
            >
              <span className="flex w-full items-center gap-2">
              {/*
               * Leading run-status dot -- the task's latest run, live off
               * run.status (Item 12). In its own reserved slot for the
               * same reason the attention slot below is reserved: a task's
               * first run must not shove the title sideways. Task.Status
               * is not the source: internal/workspace moves a task
               * created -> running on its first run and never back, so it
               * means "has ever run", not "is running now".
               */}
              <span data-testid="task-run-status-slot" className="flex w-2.5 shrink-0 items-center justify-center">
                {runDot && (
                  <StatusDot
                    status={runDot}
                    data-testid="task-run-status"
                    data-run-status={runState}
                    aria-label={`latest run ${runState}`}
                  />
                )}
              </span>
              <span className="min-w-0 truncate">{task.Title}</span>
              {/*
               * The attention-dot slot is always in the DOM at a fixed
               * width, whether or not there's anything to show -- only
               * the dot *inside* it is conditional (queried by testid
               * elsewhere, so it must stay absent-when-none). Without the
               * reserved slot, the status text below shifts left/right by
               * the dot's width whenever attention arrives or clears --
               * exactly the "changing state must not move the layout"
               * rule from refs/paseo/docs/design.md §11 that Item 2 calls
               * out for this row specifically.
               *
               * The dot's variant now names *why* the task wants
               * attention (Item 12): error, permission and finished each
               * get their own, where all three used to render the same
               * warning dot. The reason is on data-attention-reason and
               * in the accessible name, so neither a test nor a screen
               * reader has to tell them apart by colour.
               */}
              <span data-testid="task-attention-slot" className="ml-auto flex w-2.5 shrink-0 items-center justify-center">
                {reason && (
                  <StatusDot
                    status={attentionDotStatus(reason)}
                    data-testid="task-attention"
                    data-attention-reason={reason}
                    aria-label={`task needs attention: ${ATTENTION_LABEL[reason]}`}
                  />
                )}
              </span>
              </span>
              <TaskMetaRow stat={stat} status={statusOverrides.get(task.ID) ?? task.Status} />
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
