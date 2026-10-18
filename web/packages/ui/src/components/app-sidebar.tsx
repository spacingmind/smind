import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentType,
  type KeyboardEvent,
  type ReactNode,
} from "react";

import { useCommands } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";
import { accountHealthTestKey } from "@/lib/provider-health";
import type { WsClient } from "@/lib/ws-client";
import type {
  AgentProfile,
  ProviderListResult,
  ProviderTestResult,
  Space,
  Task,
  TaskStat,
  TaskStatusEventPayload,
  Workspace,
} from "@/lib/types";
import {
  applyLifecycleEvent,
  buildWorkspaceTree,
  LIFECYCLE_TOPICS,
  recentWorkspaceParentDirs,
  searchTasks,
  type SpaceWithTasks,
  type TaskSearchResult,
  type WorkspaceWithTree,
} from "@/lib/workspace-tree";
import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import { aggregateStatus, attentionDotStatus, primaryAttentionReason, runDotStatus, taskSlotSignal, workspaceMonogram, workspaceTasks } from "@/lib/sidebar-signal";
import { attentionRollup, nestTasks, type TaskNode } from "@/lib/task-tree";
import { sidebarTreeActions, type TreeActionAvailability } from "@/lib/task-tree-actions";
import { ArchiveFinishedDialog, StopTreeDialog } from "@/components/tree-action-dialogs";
import { formatRelativeTime, formatRelativeTimeShort } from "@/lib/relative-time";
import { useTaskStats, type TaskStats } from "@/hooks/use-task-stats";
import { useAttentionNotifications } from "@/hooks/use-attention-notifications";
import { useNotificationPermission } from "@/hooks/use-notification-permission";
import { useNotificationSoundPreference } from "@/hooks/use-notification-sound-preference";
import { usePinnedTasks } from "@/hooks/use-pinned-tasks";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import { useCollapsedTasks } from "@/hooks/use-collapsed-tasks";
import { useCollapsedNodes, type CollapsedNodes } from "@/hooks/use-collapsed-nodes";
import { useRowActive } from "@/hooks/use-row-active";
import { useSidebarGroupMode } from "@/hooks/use-sidebar-group-mode";
import { groupTasksByStatus, type StatusGroup } from "@/lib/sidebar-status-groups";
import type { ConnectionStatus } from "@/lib/reconnect";
import { UsageSidebarItem } from "@/components/account-usage/usage-sidebar-item";
import { SidebarFooterActions } from "@/components/sidebar-footer";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import {
  ArchiveTaskDialog,
  CreateSpaceDialog,
  CreateTaskDialog,
  CreateWorkspaceDialog,
  DeleteSpaceDialog,
  DeleteWorkspaceDialog,
} from "@/components/crud-dialogs";
import { SetupBadge } from "@/components/task-setup";
import { WorkspaceScriptsDialog } from "@/components/workspace-scripts-dialog";
import { Button } from "@/components/ui/button";
import { TooltipProvider } from "@/components/ui/tooltip";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { FadeText } from "@/components/ui/fade-text";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "@/components/ui/toast";
import { useLocalPathActions, type LocalPathActions } from "@/hooks/use-local-path-actions";
import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS, isDesktop } from "@/lib/platform";
import { TRAFFIC_LIGHT_CLUSTER_PX, hasTrafficLights, toggleZoneEndPx } from "@/lib/window-chrome";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  useSidebar,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
} from "@/components/ui/sidebar";

/**
 * Task rows indent by their own left padding, not by nesting margins on the
 * task-tree lists they live in (sidebar-polish S7): a row's hover/selected
 * highlight then spans its container's content box at every depth, and the
 * title keeps its width. A root task row starts flush with its container's
 * content box (ROW_INSET_PX = 0: the container's own `pl-1` is the inset),
 * plus the 12px chevron gutter (ROW_GUTTER_PX) ONLY when some root in the
 * same list has children -- reserved per list so a parent's chevron has room
 * and its siblings' dots stay in one column, but not spent on the (common)
 * flat list, where it cost ~12px of a ~110px title in a 256px sidebar
 * (real-app finding, 2026-10-12). The trade-off is accepted: a root title can
 * sit 12px apart between a list with a parent and one without (sibling spaces,
 * or the flat pinned/search/status views). Each level of the task tree adds
 * 10px (ROW_INDENT_PX).
 *
 * Container nesting is drawn differently (ZCode's workspace-grouped-tasks):
 * the expanded content of a WORKSPACE, SPACE (or "Ungrouped"), Pinned or
 * status-group header sits behind ONE subtle guide line under the parent
 * header's icon centre (CONTAINER_GUIDE_CLASS via ContainerContent), so
 * spaces read as nested under their workspace and tasks under their space;
 * rows inside keep their own padding, so their highlight spans from the
 * guide line to the sidebar's right edge. The task tree itself never
 * carries guide lines -- only per-row padding.
 */
const ROW_INSET_PX = 0;
const ROW_INDENT_PX = 10;
const ROW_GUTTER_PX = 12;
/** Where a depth-0 task row's status slot starts in a list where no row has children. */
const ROOT_PAD_PX = ROW_INSET_PX;
/** Where it starts in a list where some root has children, so the chevron has a gutter. */
const ROOT_PAD_WITH_PARENT_PX = ROW_INSET_PX + ROW_GUTTER_PX;

/**
 * Applied to every nested SidebarMenuSub: the list itself adds no indent,
 * margin or guide line -- rows carry their own. Container content lists
 * (ContainerContent) re-add exactly one guide line on top of this.
 */
const FLUSH_SUB = "mx-0 translate-x-0 gap-0.5 border-l-0 px-0 py-0.5";

/**
 * The one guide line a container's expanded content carries (ZCode's
 * TASK_GROUP_CONTENT_CLASS, narrowed from ml-4 + pl-2 = 25px so a 192px
 * sidebar keeps its title width): the line sits under the parent header's
 * icon centre (px-2 + half the 16px icon = 16px, hence ml-4) and content
 * pads 4px past it. The border colour is SidebarMenuSub's own subtle
 * border-sidebar-border -- only the width was zeroed by FLUSH_SUB.
 */
const CONTAINER_GUIDE_CLASS = "ml-4 border-l pl-1";

/** Stable empty fallback for AppSidebar's own `attention?: TaskAttention` (optional prop -- e.g. tests that don't wire one up) -- a literal `new Map()` inline would be a fresh reference every render, needlessly re-running useAttentionNotifications' effect. */
const EMPTY_ATTENTION: TaskAttention = new Map();

/** Stable empty fallback for the optional `runStatus` prop, for the same reason EMPTY_ATTENTION exists: a literal `new Map()` inline would re-run the row-signal memo every render. */
const EMPTY_RUN_STATUS: TaskRunStatus = new Map();

/** Stable empty fallback for the optional `unread` prop, same reasoning as EMPTY_RUN_STATUS. */
const EMPTY_UNREAD: ReadonlySet<number> = new Set();

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
  /** Task ids the user hasn't opened since their last attention-worthy event, or a manual "Mark unread" (AC2). */
  unread: ReadonlySet<number>;
  /** The task context menu's "Mark unread" action -- lives on the context (like the rest of this interface) rather than threaded as a prop through every intermediate row component. */
  onMarkUnread: (taskId: number) => void;
  /** Task ids pinned to the sidebar's Pinned section (AC4). */
  pinned: ReadonlySet<number>;
  /** Wall clock (ms), refreshed once a minute, so the rows' relative times age without every row owning a timer. */
  now: number;
  /** The task context menu's Pin/Unpin action. */
  onTogglePin: (taskId: number) => void;
}

const EMPTY_SIGNAL: RowSignal = {
  statusOverrides: new Map(),
  runStatus: new Map(),
  stats: new Map(),
  unread: new Set(),
  onMarkUnread: () => {},
  pinned: new Set(),
  now: 0,
  onTogglePin: () => {},
};

const RowSignalContext = createContext<RowSignal>(EMPTY_SIGNAL);

/** Persisted container expansion (S8), shared with the nested containers the same way as the row signal. */
const NODE_COLLAPSE_FALLBACK: CollapsedNodes = {
  collapsed: new Set(),
  isOpen: () => true,
  toggle: () => {},
  open: () => {},
};
const CollapsedNodesContext = createContext<CollapsedNodes>(NODE_COLLAPSE_FALLBACK);

function useNodeOpen(id: string): [boolean, () => void] {
  const { isOpen, toggle } = useContext(CollapsedNodesContext);
  return [isOpen(id), useCallback(() => toggle(id), [toggle, id])];
}

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

/**
 * Tree-wide data for PR-H11's tree actions: per-task availability of
 * "Archive finished sub-tasks" / "Stop all in tree" (computed once from the
 * full task list + run statuses, keyed only for tasks with live
 * descendants) and the handlers that open the two confirm dialogs. Carried
 * by context because the rows sit four component levels below AppSidebar
 * and none of the intermediates needs it.
 */
const TreeActionsContext = createContext<{
  availability: ReadonlyMap<number, TreeActionAvailability>;
  onArchiveFinished: (task: Task) => void;
  onStopTree: (task: Task) => void;
} | null>(null);

/** Which create/archive dialog a sidebar row has opened, if any. */
type CrudTarget =
  | { kind: "workspace" }
  | { kind: "space"; workspace: WorkspaceWithTree }
  | { kind: "task"; workspace: WorkspaceWithTree; spaceId: number | null }
  | { kind: "archive"; task: Task }
  | { kind: "archiveFinished"; task: Task }
  | { kind: "stopTree"; task: Task }
  | { kind: "scripts"; workspace: WorkspaceWithTree }
  | { kind: "deleteWorkspace"; workspace: WorkspaceWithTree }
  | { kind: "deleteSpace"; space: SpaceWithTasks };

export function AppSidebar({
  client,
  selectedTaskId = null,
  onSelectTask,
  attention,
  runStatus,
  unread,
  onMarkUnread,
  events,
  onTasksChange,
  onWorkspacesChange,
  onOpenSettings,
  connectionStatus = "connected",
  connectionStatusText,
  headerActions,
  windowToggle = false,
}: {
  client: WsClient | null;
  /** The desktop shell's window-level toggle floats over this sidebar's top-left (`WindowSidebarToggle`): the expanded strip pads past it and the rail keeps an empty h-12 strip under it. */
  windowToggle?: boolean;
  /** Right end of the expanded top strip: the shell's sidebar toggle and update badge (ZCode's left zone). Absent while the sidebar is a rail or a sheet -- the main header hosts them then. */
  headerActions?: ReactNode;
  /** The currently-selected task's id, if any, so its row can render as active. */
  selectedTaskId?: number | null;
  /** Invoked with the full Task when a task row is clicked. */
  onSelectTask?: (task: Task) => void;
  /** Per-task attention badges (App.tsx's useTaskAttention) -- renders a dot on each row that has any reason. */
  attention?: TaskAttention;
  /** Per-task latest-run status (App.tsx's useTaskAttention, same hook) -- renders the row's leading run dot and feeds the container rows' aggregate. */
  runStatus?: TaskRunStatus;
  /** Task ids the user hasn't opened since their last attention-worthy event (App.tsx's useUnreadTasks) -- bolds the row's title (AC2). */
  unread?: ReadonlySet<number>;
  /** Manual "Mark unread" override from the task's context menu (App.tsx's useUnreadTasks). */
  onMarkUnread?: (taskId: number) => void;
  /** The app's single-subscription event stream -- drives live task.status overrides. Optional so tests/mounts without it render as before. */
  events?: DaemonEvents | null;
  /**
   * Called with every task across the whole tree whenever the tree
   * changes. The shell needs a flat task list for things the sidebar
   * itself doesn't do -- `task.prev`/`task.next` shortcuts, the command
   * palette's task entries, restoring a task id from the URL -- and this
   * component is the one place that already fetches it. Optional: mounts
   * that don't care (every existing test) behave exactly as before.
   */
  onTasksChange?: (tasks: Task[]) => void;
  /** Called with the workspace list whenever the tree changes -- same rationale as onTasksChange, for the palette's workspace entries. */
  onWorkspacesChange?: (workspaces: Workspace[]) => void;
  /**
   * Opens the settings screen. Settings is a full-pane view owned by the
   * shell (App.tsx), so the sidebar just forwards the click -- see
   * SettingsScreen's doc comment for why the screen lives above this
   * component. The optional section id deep-links the screen (App.tsx's
   * settingsInitialSectionId) -- the Providers entry points pass
   * "providers".
   */
  onOpenSettings?: (sectionId?: string) => void;
  /** The daemon connection state and its human text (App.tsx's STATUS_LABEL / connect error) -- the footer's Host popover shows it. */
  connectionStatus?: ConnectionStatus;
  connectionStatusText?: string;
}) {
  const { workspaces, error, refresh } = useWorkspaceTree(client, events ?? null);
  const statusOverrides = useStatusOverrides(client, events ?? null);
  const workspaceIds = useMemo(() => (workspaces ?? []).map((ws) => ws.ID), [workspaces]);
  const stats = useTaskStats(client, events ?? null, workspaceIds);

  // Out-of-tab attention notifications (Item 4): every task across the
  // whole tree, flattened just far enough to label a Notification by
  // title -- this list changing (workspace/space/task fetch completing)
  // never itself fires anything; only a *new* attention reason while the
  // tab is hidden does, in the hook itself.
  const allTasks = useMemo(
    () => (workspaces ?? []).flatMap((ws) => [...ws.spaces.flatMap((sp) => sp.tasks), ...ws.ungroupedTasks]),
    [workspaces],
  );
  // null until the tree's first successful load (workspaces !== null), so
  // an archived/deleted task can be pruned from `pinned` without the
  // tree's empty *initial* render wiping a persisted pin out before the
  // real fetch even lands -- see usePinnedTasks' own doc comment.
  const liveTaskIds = useMemo(
    () => (workspaces === null ? null : new Set(allTasks.map((t) => t.ID))),
    [workspaces, allTasks],
  );
  const { pinned, togglePin } = usePinnedTasks(liveTaskIds);
  // Collapsed-parent state for the tree view (PR-U1), pruned against the
  // same live set as pins. Default empty: every parent starts expanded so
  // a freshly loaded workspace shows its hierarchy rather than looking
  // mysteriously empty.
  const { collapsed, toggle: toggleCollapsed, expand: expandCollapsed } = useCollapsedTasks(liveTaskIds);
  // The task hierarchy for the tree view (PR-U1): built from ALL of the
  // workspace's tasks at once because the hierarchy trumps the space
  // grouping (plan Decision 2) -- a child nests under its parent even when
  // its own SpaceID differs, so per-space nesting would be wrong. Each
  // space bucket filters these roots back down to its own afterwards.
  const taskForest = useMemo(() => nestTasks(allTasks), [allTasks]);
  const treeActionAvailability = useMemo(
    () => sidebarTreeActions(allTasks, runStatus ?? EMPTY_RUN_STATUS),
    [allTasks, runStatus],
  );
  // A selected task hidden under a collapsed parent must be revealed, or
  // the keyboard/palette "select task" flows would silently select
  // something the user cannot see. Only the collapsed ancestors on the
  // selected node's chain are expanded -- the user's other collapse
  // choices survive.
  useEffect(() => {
    if (selectedTaskId == null) return;
    const chain: number[] = [];
    const walk = (nodes: TaskNode[]): boolean => {
      for (const node of nodes) {
        if (node.task.ID === selectedTaskId) return true;
        chain.push(node.task.ID);
        if (walk(node.children)) return true;
        chain.pop();
      }
      return false;
    };
    if (!walk(taskForest)) return;
    const hidden = chain.filter((id) => collapsed.has(id));
    if (hidden.length > 0) expandCollapsed(hidden);
  }, [selectedTaskId, taskForest, collapsed, expandCollapsed]);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(id);
  }, []);
  // Which workspaces/spaces/Pinned/status groups the user has collapsed (S8).
  // Everything defaults open, so a new workspace or space needs no entry.
  const liveNodeIds = useMemo(
    () =>
      workspaces === null
        ? null
        : new Set(
            workspaces.flatMap((ws) => [`ws:${ws.ID}`, `ungrouped:${ws.ID}`, ...ws.spaces.map((sp) => `space:${sp.ID}`)]),
          ),
    [workspaces],
  );
  const nodes = useCollapsedNodes(liveNodeIds);
  const rowSignal = useMemo<RowSignal>(
    () => ({
      now,
      attention,
      statusOverrides,
      runStatus: runStatus ?? EMPTY_RUN_STATUS,
      stats,
      unread: unread ?? EMPTY_UNREAD,
      onMarkUnread: onMarkUnread ?? EMPTY_SIGNAL.onMarkUnread,
      pinned,
      onTogglePin: togglePin,
    }),
    [now, attention, statusOverrides, runStatus, stats, unread, onMarkUnread, pinned, togglePin],
  );

  const pinnedTasks = useMemo(() => allTasks.filter((t) => pinned.has(t.ID)), [allTasks, pinned]);
  const { groupMode, setGroupMode } = useSidebarGroupMode();
  const statusGroups = useMemo(
    () => groupTasksByStatus(allTasks, attention ?? EMPTY_ATTENTION, runStatus ?? EMPTY_RUN_STATUS),
    [allTasks, attention, runStatus],
  );
  const { permission: notificationPermission } = useNotificationPermission();
  const { enabled: notificationSoundEnabled } = useNotificationSoundPreference();
  const openNotifiedTask = useCallback(
    (taskId: number) => {
      const task = allTasks.find((t) => t.ID === taskId);
      if (task) onSelectTask?.(task);
    },
    [allTasks, onSelectTask],
  );
  useAttentionNotifications(
    attention ?? EMPTY_ATTENTION,
    allTasks,
    notificationPermission,
    openNotifiedTask,
    notificationSoundEnabled,
  );

  useEffect(() => {
    onTasksChange?.(allTasks);
  }, [allTasks, onTasksChange]);

  useEffect(() => {
    if (workspaces) onWorkspacesChange?.(workspaces);
  }, [workspaces, onWorkspacesChange]);

  const { inSidebar: showUsageSummary } = useUsagePreferences();
  const [crud, setCrud] = useState<CrudTarget | null>(null);
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);

  // Profiles feed the footer's agent count and the palette's "Use agent:"
  // entries. The composer fetches its own copy (its picker seeds on it);
  // sharing one fetch would couple the two surfaces for the sake of one
  // RPC that the daemon answers from its store. Failures fall back to an
  // empty list -- both consumers already handle zero profiles.
  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<AgentProfile[]>("profile.list")
      .then((list) => {
        if (!cancelled) setProfiles(list ?? []);
      })
      .catch((err) => console.error("profile.list failed, hiding agent palette entries", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  // The footer's Providers row health dot + "N healthy" count (run-config
  // IA plan) -- reuses the provider.test-based pattern profiles-section.tsx
  // built for its own per-agent health dot (accountHealthTestKey resolves
  // each provider's id to whichever vocabulary provider.test expects).
  // Fetched (and tested) once per client, not on every render or on a
  // polling interval -- `providerHealth` is the cache this effect fills,
  // and the deps array of just `[client]` is what keeps it from re-testing
  // on unrelated re-renders.
  const [providerHealth, setProviderHealth] = useState<{ healthy: number; total: number } | null>(null);
  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then(async (result) => {
        if (cancelled) return;
        const providers = result.providers;
        const oks = await Promise.all(
          providers.map((p) =>
            client
              .call<ProviderTestResult>("provider.test", { provider: accountHealthTestKey(p.id, providers) })
              .then((r) => r.ok)
              .catch(() => false),
          ),
        );
        if (!cancelled) setProviderHealth({ healthy: oks.filter(Boolean).length, total: providers.length });
      })
      .catch((err) => console.error("provider.list failed, hiding the footer's provider health count", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  /**
   * Moving a task has no confirmation dialog (unlike archive/delete -- it's
   * reversible, just another move away), so it's a direct RPC call from the
   * dropdown rather than a CrudTarget. `refresh()` on success mirrors every
   * other mutation here (see useWorkspaceTree's doc comment): the daemon's
   * own task.updated event already splices the task into its new location,
   * but the acting client doesn't rely solely on its own events.subscribe
   * having succeeded.
   */
  const moveTask = useCallback(
    async (task: Task, spaceId: number | null) => {
      if (!client) return;
      try {
        await client.call("task.move", { id: task.ID, spaceId });
        refresh();
      } catch (err) {
        toast({
          variant: "error",
          title: `Couldn't move “${task.Title}”`,
          description: err instanceof Error ? err.message : String(err),
        });
      }
    },
    [client, refresh],
  );

  // Command-palette contributions for the dialogs this component owns.
  // Registered here rather than in App.tsx on purpose: the surface that
  // owns a dialog is the one that can open it, and `useCommands` is the
  // API that lets it contribute without either file importing the other
  // (docs/design.md §8).
  const paletteCommands = useMemo<Command[]>(() => {
    const commands: Command[] = [
      {
        id: "new-workspace",
        group: "Actions",
        title: "New workspace",
        keywords: ["create", "add", "project", "repo"],
        run: () => setCrud({ kind: "workspace" }),
      },
      {
        id: "accounts",
        group: "Actions",
        title: "Settings: Providers",
        keywords: ["providers", "accounts", "credentials", "login", "oauth"],
        run: () => onOpenSettings?.("providers"),
      },
      {
        id: "settings-agents",
        group: "Settings",
        title: "Settings: Agents",
        keywords: ["profiles", "agents"],
        action: "settings.open",
        run: () => window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "agents" } })),
      },
      {
        id: "new-agent",
        group: "Settings",
        title: "New agent…",
        keywords: ["create", "profile", "agent"],
        run: () => window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: "agents" } })),
      },
    ];
    // "Use agent: <name>" seeds the active composer with that profile's
    // config, exactly as picking it from the composer's own picker does
    // (ADR-0014's client-side apply mechanism). Dispatched as a window
    // event because the composer is not a child of the sidebar -- the
    // shell would otherwise have to thread a ref through task panes for
    // this one interaction.
    for (const p of profiles) {
      commands.push({
        id: `use-agent-${p.ID}`,
        group: "Agents",
        title: `Use agent: ${p.Name}`,
        keywords: ["agent", "profile", p.Provider],
        run: () => window.dispatchEvent(new CustomEvent("smind:use-agent", { detail: p })),
      });
    }
    // "New task" needs a workspace to create the task in. With exactly one
    // workspace the choice is unambiguous; with several, picking one for
    // the user would be a guess, so the entry is per workspace instead.
    for (const ws of workspaces ?? []) {
      commands.push({
        id: `new-task-${ws.ID}`,
        group: "Actions",
        title:
          (workspaces ?? []).length === 1
            ? "New task"
            : `New task in ${ws.Title || ws.Path}`,
        keywords: ["create", "add", ws.Title, ws.Path],
        run: () => setCrud({ kind: "task", workspace: ws, spaceId: null }),
      });
    }
    return commands;
  }, [workspaces, profiles, onOpenSettings]);
  useCommands("sidebar:actions", 5, paletteCommands);
  // Collapsed search (audit-deepseek-harness.md §3): a header action that
  // expands into a field. An outside click collapses it only while the
  // query is empty -- a typed query survives clicking away, because
  // dismissing someone's search by accident is worse than leaving a field
  // open. The tree's `expanded` set is never touched while searching, so
  // clearing the query restores exactly the expansion state it had.
  const { fullscreen } = useWindowState();
  const localPathActions = useLocalPathActions();
  const lights = isDesktop && hasTrafficLights(desktopOS, fullscreen);
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
      {/*
       * Item 1 (collapsed dead space): the header renders two variants.
       * Expanded: wordmark left, theme/settings/accounts buttons right.
       * Icon-collapsed: a single centered stack of the same buttons --
       * previously the row merely hid its children
       * (`group-data-[collapsible=icon]:hidden`) while the row and its
       * padding kept occupying vertical space, leaving a phantom gap
       * under the rail. Rendering the buttons (rather than removing the
       * header) keeps every affordance reachable in collapsed mode.
       */}
      <SidebarHeader className="gap-0 p-0">
        <div
          data-testid="sidebar-expanded-header"
          // The sidebar's top strip is ZCode's "left zone": the same 48px as the
          // main header beside it, so logo and title share one centre line.
          // Desktop: it is also the window's title bar over the sidebar --
          // left-padded past the native traffic lights and the window-level
          // toggle (or just the lights in a compact sheet), draggable around
          // its buttons.
          data-tauri-drag-region={windowToggle || lights ? "deep" : undefined}
          className="@container/strip flex h-12 items-center gap-2 px-2 transition-[padding-left] duration-200 ease-out group-data-[collapsible=icon]:hidden motion-reduce:transition-none"
          style={
            windowToggle
              ? { paddingLeft: toggleZoneEndPx(desktopOS, fullscreen) }
              : lights
                ? { paddingLeft: TRAFFIC_LIGHT_CLUSTER_PX }
                : undefined
          }
        >
          <span
            data-testid="sidebar-logo"
            className="grid size-6 shrink-0 place-items-center rounded-md bg-foreground dark:size-5 dark:bg-transparent"
          >
            <img src="/logo.png" alt="" className="size-4 dark:size-5" />
          </span>
          {/* Past the lights/toggle zone (the query measures the strip's content box, i.e. what is left of the width after that padding) the strip can be too narrow for the wordmark: drop it then (rather than truncating to "s…"). */}
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-ui-base font-semibold tracking-tight",
              (windowToggle || lights) && "hidden @min-[7rem]/strip:block",
            )}
          >
            sMind
          </span>
          {(windowToggle || lights) && <div className="min-w-0 flex-1 @min-[7rem]/strip:hidden" />}
          {headerActions && <div className="flex shrink-0 items-center gap-1">{headerActions}</div>}
        </div>
        {(windowToggle || lights) && (
          // Collapsed rail: an empty h-12 strip for the window-level toggle (and,
          // on macOS, the lights) to sit on, so the rail's icons start below
          // them. No border -- the main header's own border carries the line
          // across.
          <div
            data-testid="sidebar-rail-inset"
            data-tauri-drag-region
            className="hidden h-12 shrink-0 group-data-[collapsible=icon]:block"
          />
        )}
      </SidebarHeader>
      {/*
       * Only the tree scrolls: SidebarContent stops being the scroller
       * (overflow-hidden) and the group/content chain is bounded
       * (min-h-0 flex-1), so the ScrollArea below gets a real height and the
       * "Workspaces" label row stays put above it. Left as the shadcn default
       * the label scrolled away with the list (real-app finding, 2026-10-12).
       */}
      <SidebarContent className="overflow-hidden">
        <SidebarGroup className="min-h-0 flex-1">
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
                className="h-6 rounded-md px-1.5 text-ui-sm"
              />
            ) : (
              <>
                <span>Workspaces</span>
                <span className="flex items-center">
                  {!empty && (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={groupMode === "tree" ? "Group by status" : "Group by workspace"}
                      aria-pressed={groupMode === "status"}
                      data-testid="sidebar-group-mode-toggle"
                      onClick={() => setGroupMode(groupMode === "tree" ? "status" : "tree")}
                    >
                      {groupMode === "tree" ? <Icons.groupByWorkspace /> : <Icons.groupByStatus />}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Search tasks"
                    data-testid="sidebar-search-toggle"
                    onClick={() => setSearchOpen(true)}
                  >
                    <Icons.search />
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
                      <Icons.add />
                    </Button>
                  )}
                </span>
              </>
            )}
          </SidebarGroupLabel>
          <SidebarGroupContent className="flex min-h-0 flex-1 flex-col">
            {/* The rail rows carry a name tooltip (SidebarMenuButton `tooltip`). */}
            <TooltipProvider>
            <ScrollArea className="min-h-0 flex-1 group-data-[collapsible=icon]:[&_[data-slot=scroll-area-scrollbar]]:hidden">
              <RowSignalContext.Provider value={rowSignal}>
              <TreeActionsContext.Provider
                value={treeActionAvailability.size > 0 ? {
                  availability: treeActionAvailability,
                  onArchiveFinished: (task) => setCrud({ kind: "archiveFinished", task }),
                  onStopTree: (task) => setCrud({ kind: "stopTree", task }),
                } : null}
              >
                <CollapsedNodesContext.Provider value={nodes}>
                <SidebarMenu className="@container/sb">
                  {error && <StatusRow icon={<Icons.error className="size-3.5" />} className="text-destructive" text={error} />}
                  {!error && workspaces === null && (
                    <StatusRow icon={<Icons.spinner className="size-3.5 animate-spin" />} text="Loading workspaces…" />
                  )}
                  {empty && !searching && (
                    <SidebarMenuItem>
                      <div className="flex flex-col gap-3 px-2 py-4">
                        <div className="text-ui-sm text-muted-foreground">
                          <p className="font-medium text-foreground">Welcome to sMind</p>
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
                          <Icons.add /> New workspace
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
                  {!searching && pinnedTasks.length > 0 && (
                    <PinnedSection
                      tasks={pinnedTasks}
                      selectedTaskId={selectedTaskId}
                      onSelectTask={onSelectTask}
                      onArchiveTask={(task) => setCrud({ kind: "archive", task })}
                    />
                  )}
                  {!searching && groupMode === "status" && (
                    <StatusGroupedList
                      groups={statusGroups}
                      selectedTaskId={selectedTaskId}
                      onSelectTask={onSelectTask}
                      onArchiveTask={(task) => setCrud({ kind: "archive", task })}
                    />
                  )}
                  {!searching &&
                    groupMode === "tree" &&
                    workspaces?.map((ws) => (
                    <WorkspaceItem
                      key={ws.ID}
                      workspace={ws}
                      forest={taskForest.filter((n) => n.task.WorkspaceID === ws.ID)}
                      collapsed={collapsed}
                      onToggleCollapse={toggleCollapsed}
                      expanded={nodes.isOpen(`ws:${ws.ID}`)}
                      onToggleExpanded={() => nodes.toggle(`ws:${ws.ID}`)}
                      onAddSpace={() => setCrud({ kind: "space", workspace: ws })}
                      onAddTask={(spaceId) => setCrud({ kind: "task", workspace: ws, spaceId })}
                      onArchiveTask={(task) => setCrud({ kind: "archive", task })}
                      onMoveTask={moveTask}
                      onEditScripts={() => setCrud({ kind: "scripts", workspace: ws })}
                      onDeleteWorkspace={() => setCrud({ kind: "deleteWorkspace", workspace: ws })}
                      onDeleteSpace={(space) => setCrud({ kind: "deleteSpace", space })}
                      localPathActions={localPathActions}
                      selectedTaskId={selectedTaskId}
                      onSelectTask={onSelectTask}
                    />
                  ))}
                </SidebarMenu>
                </CollapsedNodesContext.Provider>
              </TreeActionsContext.Provider>
              </RowSignalContext.Provider>
            </ScrollArea>
            </TooltipProvider>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      {/*
       * Footer (Paseo-style icon line): add workspace, usage, host (connection
       * + Providers/Agents health popover), help, theme, settings.
       */}
      <SidebarFooter className="p-0">
        <SidebarFooterActions
          rows={showUsageSummary ? <UsageSidebarItem client={client} events={events ?? null} /> : undefined}
          connectionStatus={connectionStatus}
          connectionStatusText={connectionStatusText}
          providerHealth={providerHealth}
          agentCount={profiles.length}
          onAddWorkspace={() => setCrud({ kind: "workspace" })}
          onOpenSettings={onOpenSettings}
        />
      </SidebarFooter>

      {client && (
        <>
          <CreateWorkspaceDialog
            client={client}
            open={crud?.kind === "workspace"}
            onOpenChange={(o) => !o && setCrud(null)}
            onCreated={(ws) => {
              // Land on the new workspace expanded, even if a deleted
              // workspace's id is still persisted as collapsed.
              nodes.open(`ws:${ws.ID}`);
              refresh();
            }}
            recentPaths={recentWorkspaceParentDirs(workspaces ?? [])}
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
          {crud?.kind === "archiveFinished" && (
            <ArchiveFinishedDialog
              client={client}
              task={crud.task}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onArchived={refresh}
            />
          )}
          {crud?.kind === "stopTree" && (
            <StopTreeDialog
              client={client}
              task={crud.task}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onStopped={refresh}
            />
          )}
          {crud?.kind === "scripts" && (
            <WorkspaceScriptsDialog
              client={client}
              workspace={crud.workspace}
              open
              onOpenChange={(o) => !o && setCrud(null)}
              onSaved={refresh}
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
        </>
      )}
    </Sidebar>
  );
}

/**
 * The flat list a non-blank query replaces the tree with
 * (audit-deepseek-harness.md §3). Rows are the same TaskRows the tree
 * renders -- same status slot, same actions menu -- so a task
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
      <SidebarMenuSub className={FLUSH_SUB}>
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

/**
 * The Pinned section (AC4): every pinned task, flattened across
 * workspaces, above the regular tree -- same TaskRows the tree itself
 * uses, so a pinned row behaves identically to its counterpart in its own
 * workspace/space (a task appears in both places; pinning doesn't remove
 * it from where it normally lives). Whether the section is collapsed is
 * persisted too (sidebar-polish S8, id `pinned`).
 */
function PinnedSection({
  tasks,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
}: {
  tasks: Task[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
}) {
  const [open, toggleOpen] = useNodeOpen("pinned");
  const { active, rowProps } = useRowActive();

  return (
    <SidebarMenuItem data-testid="sidebar-pinned-section">
      <div {...rowProps}>
        <SidebarMenuButton
          onClick={toggleOpen}
          aria-expanded={open}
          data-testid="sidebar-pinned-section-header"
          className="rounded-lg"
        >
          <Icons.pin className={cn("size-3.5", active ? "text-foreground" : "text-foreground-subtle")} />
          <FadeText
            data-testid="container-title"
            className={cn("flex-initial text-ui-sm", active ? "text-foreground" : "text-foreground-subtle")}
          >
            Pinned
          </FadeText>
          <RowChevron open={open} />
        </SidebarMenuButton>
      </div>
      {open && (
        <ContainerContent kind="pinned">
          <TaskRows
            tasks={tasks}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No pinned tasks"
            onArchiveTask={onArchiveTask}
          />
        </ContainerContent>
      )}
    </SidebarMenuItem>
  );
}

/**
 * The group-by-status view (AC6): one collapsible bucket per
 * lib/sidebar-status-groups.ts group, each rendering its tasks through the
 * same TaskRows the tree view uses -- replaces the workspace/space tree
 * entirely while active (Pinned stays above either view, per app-sidebar's
 * render order). Each bucket's open/closed state is persisted like
 * PinnedSection's (sidebar-polish S8, id `status:<key>`).
 */
function StatusGroupedList({
  groups,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
}: {
  groups: StatusGroup[];
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
}) {
  return (
    <>
      {groups.map((group) => (
        <StatusGroupItem
          key={group.key}
          group={group}
          selectedTaskId={selectedTaskId}
          onSelectTask={onSelectTask}
          onArchiveTask={onArchiveTask}
        />
      ))}
    </>
  );
}

function StatusGroupItem({
  group,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
}: {
  group: StatusGroup;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
}) {
  const [open, toggleOpen] = useNodeOpen(`status:${group.key}`);
  const { active, rowProps } = useRowActive();

  return (
    <SidebarMenuItem data-testid="sidebar-status-group" data-status-group={group.key}>
      <div {...rowProps}>
        <SidebarMenuButton
          onClick={toggleOpen}
          aria-expanded={open}
          data-testid="sidebar-status-group-header"
          className="rounded-lg"
        >
          <FadeText
            data-testid="container-title"
            className={cn("flex-initial text-ui-sm", active ? "text-foreground" : "text-foreground-subtle")}
          >
            {group.label}
          </FadeText>
          <RowChevron open={open} />
          {/* A pill, as in ZCode's group headers (TASK_GROUP_COUNT_BADGE_CLASS). */}
          <span
            data-testid="status-group-count"
            className="ml-auto inline-flex min-w-5 shrink-0 items-center justify-center rounded-full bg-tag/50 px-1.5 py-0.5 text-ui-sm font-medium leading-none text-foreground-subtle"
          >
            {group.tasks.length}
          </span>
        </SidebarMenuButton>
      </div>
      {open && (
        <ContainerContent kind="status">
          <TaskRows
            tasks={group.tasks}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No tasks"
            onArchiveTask={onArchiveTask}
          />
        </ContainerContent>
      )}
    </SidebarMenuItem>
  );
}

function StatusRow({ icon, text, className }: { icon?: ReactNode; text: string; className?: string }) {
  return (
    <SidebarMenuItem>
      <div className={cn("flex items-center gap-2 px-2 py-1.5 text-ui-sm text-muted-foreground", className)}>
        {icon}
        <span>{text}</span>
      </div>
    </SidebarMenuItem>
  );
}

/**
 * The expanded content of a container header (workspace, space, "Ungrouped",
 * Pinned, status group): the same flush list as the task tree, plus ONE
 * subtle guide line under the parent header's icon (CONTAINER_GUIDE_CLASS)
 * so the content reads as nested under that header. Workspace headers
 * themselves still span the full sidebar width; only their content is
 * inset. Carries a stable testid so tests can tell container content apart
 * from the flush task-tree lists.
 */
function ContainerContent({ kind, children }: { kind: string; children: ReactNode }) {
  // A space's (or "Ungrouped") content carries NO guide of its own at any
  // width: the workspace's guide already frames it, and a second ~21px level
  // left a depth-0 title ~88px from the edge of a 256px sidebar -- ~110px of
  // title, so nearly every real title was cut. Tasks stay flush under the space
  // header and rely on its icon/weight to read as nested (their status dots
  // line up under the header's icon).
  const isSpaceLike = kind === "space" || kind === "ungrouped";
  return (
    <SidebarMenuSub
      data-testid="sidebar-container-content"
      data-container-kind={kind}
      className={cn(FLUSH_SUB, !isSpaceLike && CONTAINER_GUIDE_CLASS)}
    >
      {children}
    </SidebarMenuSub>
  );
}

/**
 * The one chevron every container header (workspace, space, Pinned, status
 * group) uses, so they cannot drift apart (sidebar-polish S5): same size, the
 * same muted colour, and always directly after the title -- where ZCode's
 * group headers put it (workspace-grouped-tasks/types.ts) -- never pushed to
 * the far edge where the row's own actions live.
 */
function RowChevron({ open }: { open: boolean }) {
  return (
    <Icons.chevronRight
      data-testid="row-chevron"
      aria-hidden="true"
      className={cn("size-3.5 shrink-0 text-foreground-subtlest transition-transform", open && "rotate-90")}
    />
  );
}

/**
 * The "⋯" menu shared by workspace and space rows. Mounted by the caller only
 * while its row is hovered/focused/menu-open (use-row-active.ts); `onOpenChange`
 * is how the row learns its menu is open so it stays mounted.
 */
function RowMenu({
  items,
  triggerProps,
  onOpenChange,
}: {
  items: { label: string; icon: ReactNode; onSelect: () => void }[];
  /** Extra DOM attributes (data-testid, data-*-id) so callers can disambiguate this shared trigger's otherwise-identical "Row actions" label between workspace and space rows. */
  triggerProps?: Record<string, string | number>;
  onOpenChange?: (open: boolean) => void;
}) {
  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="Row actions" className="size-5 p-0" {...triggerProps}>
          <Icons.more />
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

/**
 * A container header's trailing action area: absolutely positioned on the
 * row's right edge, a fixed 20px box that is empty until the row is active, so
 * the ⋯ appearing never moves anything (and the button reserves the same space
 * with its own right padding, so the aggregate dot and chevron never sit under it).
 */
function RowActions({ children }: { children: ReactNode }) {
  return (
    <span className="absolute inset-y-0 right-1 flex w-5 items-center justify-center group-data-[collapsible=icon]:hidden">
      {children}
    </span>
  );
}

function WorkspaceItem({
  workspace,
  forest,
  collapsed,
  onToggleCollapse,
  expanded,
  onToggleExpanded,
  onAddSpace,
  onAddTask,
  onArchiveTask,
  onMoveTask,
  onEditScripts,
  onDeleteWorkspace,
  onDeleteSpace,
  localPathActions,
  selectedTaskId,
  onSelectTask,
}: {
  workspace: WorkspaceWithTree;
  /** This workspace's root TaskNodes (the caller already filtered the global forest by WorkspaceID). */
  forest: TaskNode[];
  /** Persisted collapsed-parent ids (PR-U1), threaded so TaskRows can render rollups on collapsed parents. */
  collapsed: ReadonlySet<number>;
  onToggleCollapse: (taskId: number) => void;
  expanded: boolean;
  onToggleExpanded: () => void;
  onAddSpace: () => void;
  onAddTask: (spaceId: number | null) => void;
  onArchiveTask: (task: Task) => void;
  onMoveTask: (task: Task, spaceId: number | null) => void;
  onEditScripts: () => void;
  onDeleteWorkspace: () => void;
  onDeleteSpace: (space: SpaceWithTasks) => void;
  /** Reveal / Open-in-editor for the workspace folder; null (items absent) unless the desktop app is on its local daemon. */
  localPathActions: LocalPathActions | null;
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
  const items = [
    { label: "Add task", icon: <Icons.add />, onSelect: () => onAddTask(null) },
    { label: "Add space", icon: <Icons.add />, onSelect: onAddSpace },
    ...(localPathActions
      ? [
          {
            label: localPathActions.revealLabel,
            icon: <Icons.folderOpen />,
            onSelect: () => localPathActions.reveal(workspace.Path),
          },
          ...localPathActions.editors.map((editor) => ({
            label: `Open in ${editor.label}`,
            icon: <Icons.externalLink />,
            onSelect: () => localPathActions.open(editor.id, workspace.Path),
          })),
        ]
      : []),
    { label: "Worktree scripts", icon: <Icons.terminal />, onSelect: onEditScripts },
    { label: "Delete workspace", icon: <Icons.delete />, onSelect: onDeleteWorkspace },
  ];

  const { active, rowProps, setMenuOpen } = useRowActive();
  const { state: sidebarState, setOpen: setSidebarOpen } = useSidebar();
  const name = workspace.Title || workspace.Path;
  const monogram = workspaceMonogram(workspace.Title || workspace.Path.split(/[\\/]/).filter(Boolean).pop() || "");
  const selectedHere = selectedTaskId !== null && workspaceTasks(workspace).some((t) => t.ID === selectedTaskId);
  // In the icon rail the row is only a badge, so a click opens the sidebar
  // and reveals this workspace's tasks rather than toggling a tree that is
  // not visible.
  const onRowClick = () => {
    if (sidebarState === "collapsed") {
      setSidebarOpen(true);
      if (!expanded) onToggleExpanded();
      return;
    }
    onToggleExpanded();
  };
  return (
    <SidebarMenuItem>
      <ContextMenu>
        {/*
          * The hover/focus scope is this wrapper -- the header line only --
          * not the li, which also contains the workspace's whole subtree.
          * It is also sticky (ZCode's grouped tasks): while the workspace's
          * tasks scroll the header stays pinned, until the next workspace's
          * header pushes it out (the li is its containing block). Space
          * headers pin just beneath it (top-8 = this header's h-8).
          */}
        <ContextMenuTrigger asChild>
          <div className="sticky top-0 z-20 bg-sidebar" {...rowProps}>
            <SidebarMenuButton
              onClick={onRowClick}
              tooltip={name}
              aria-label={name}
              data-testid="sidebar-workspace-row"
              data-workspace-id={workspace.ID}
              aria-expanded={expanded}
              className={cn("rounded-lg", active && "pr-7")}
            >
              <Icons.workspace className="group-data-[collapsible=icon]:hidden" />
              <span
                data-testid="rail-monogram"
                data-selected={selectedHere || undefined}
                className={cn(
                  "relative -m-1 hidden size-6 shrink-0 items-center justify-center rounded-md bg-muted text-ui-xs font-semibold text-foreground group-data-[collapsible=icon]:flex",
                  selectedHere && "bg-selected ring-2 ring-sidebar-ring",
                )}
              >
                <span aria-hidden="true">{monogram}</span>
                {aggregate && (
                  <StatusDot
                    status={aggregate}
                    data-testid="rail-aggregate"
                    data-aggregate={aggregate}
                    className="absolute -right-0.5 -bottom-0.5 size-2 ring-2 ring-sidebar"
                  />
                )}
              </span>
              <FadeText
                data-testid="container-title"
                fade={active}
                className="flex-initial text-ui-base font-medium group-data-[collapsible=icon]:hidden"
              >
                {name}
              </FadeText>
              <span className="contents group-data-[collapsible=icon]:hidden">
                <RowChevron open={expanded} />
                <AggregateSlot
                  status={aggregate}
                  label={`${name} activity`}
                  className={active ? "invisible" : undefined}
                />
              </span>
            </SidebarMenuButton>
            <RowActions>
              {active && (
                <RowMenu
                  items={items}
                  onOpenChange={setMenuOpen}
                  triggerProps={{
                    "data-testid": "sidebar-workspace-actions-trigger",
                    "data-workspace-id": workspace.ID,
                  }}
                />
              )}
            </RowActions>
          </div>
        </ContextMenuTrigger>
        <ContextMenuContent data-testid="sidebar-workspace-context-menu">
          {items.map((item) => (
            <ContextMenuItem key={item.label} onSelect={item.onSelect}>
              {item.icon}
              {item.label}
            </ContextMenuItem>
          ))}
        </ContextMenuContent>
      </ContextMenu>
      {expanded && (
        <ContainerContent kind="workspace">
          {flat ? (
            <TaskRows
              tasks={workspace.ungroupedTasks}
              nodes={forest.filter((n) => n.task.SpaceID === null)}
              collapsed={collapsed}
              onToggleCollapse={onToggleCollapse}
              selectedTaskId={selectedTaskId}
              onSelectTask={onSelectTask}
              emptyText="No tasks"
              onArchiveTask={onArchiveTask}
              spaces={workspace.spaces}
              onMoveTask={onMoveTask}
            />
          ) : (
            <>
              {workspace.spaces.map((space) => (
                <SpaceItem
                  key={space.ID}
                  space={space}
                  spaces={workspace.spaces}
                  forest={forest}
                  collapsed={collapsed}
                  onToggleCollapse={onToggleCollapse}
                  selectedTaskId={selectedTaskId}
                  onSelectTask={onSelectTask}
                  onAddTask={onAddTask}
                  onArchiveTask={onArchiveTask}
                  onMoveTask={onMoveTask}
                  onDeleteSpace={onDeleteSpace}
                />
              ))}
              {workspace.ungroupedTasks.length > 0 && (
                <SpaceLikeItem
                  nodeId={`ungrouped:${workspace.ID}`}
                  title="Ungrouped"
                  tasks={workspace.ungroupedTasks}
                  nodes={forest.filter((n) => n.task.SpaceID === null)}
                  collapsed={collapsed}
                  onToggleCollapse={onToggleCollapse}
                  selectedTaskId={selectedTaskId}
                  onSelectTask={onSelectTask}
                  onArchiveTask={onArchiveTask}
                  spaces={workspace.spaces}
                  onMoveTask={onMoveTask}
                />
              )}
            </>
          )}
        </ContainerContent>
      )}
    </SidebarMenuItem>
  );
}

function SpaceItem({
  space,
  spaces,
  forest,
  collapsed,
  onToggleCollapse,
  selectedTaskId,
  onSelectTask,
  onAddTask,
  onArchiveTask,
  onMoveTask,
  onDeleteSpace,
}: {
  space: SpaceWithTasks;
  /** Every space in the workspace (including this one), so its task rows can offer the others as move targets. */
  spaces: SpaceWithTasks[];
  /** This workspace's root TaskNodes; this space keeps the roots whose task.SpaceID matches it. */
  forest: TaskNode[];
  collapsed: ReadonlySet<number>;
  onToggleCollapse: (taskId: number) => void;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onAddTask: (spaceId: number | null) => void;
  onArchiveTask: (task: Task) => void;
  onMoveTask: (task: Task, spaceId: number | null) => void;
  onDeleteSpace: (space: SpaceWithTasks) => void;
}) {
  return (
    <SpaceLikeItem
      title={space.Title}
      tasks={space.tasks}
      nodes={forest.filter((n) => n.task.SpaceID === space.ID)}
      collapsed={collapsed}
      onToggleCollapse={onToggleCollapse}
      selectedTaskId={selectedTaskId}
      onSelectTask={onSelectTask}
      onArchiveTask={onArchiveTask}
      spaces={spaces}
      onMoveTask={onMoveTask}
      testId="sidebar-space-row"
      spaceId={space.ID}
      nodeId={`space:${space.ID}`}
      menuItems={[
        { label: "Add task", icon: <Icons.add />, onSelect: () => onAddTask(space.ID) },
        { label: "Delete space", icon: <Icons.delete />, onSelect: () => onDeleteSpace(space) },
      ]}
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
  nodes,
  collapsed,
  onToggleCollapse,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
  spaces,
  onMoveTask,
  menuItems,
  testId,
  spaceId,
  nodeId,
}: {
  title: string;
  /** The bucket's own tasks, still used for the aggregate dot above the list. */
  tasks: Task[];
  /** The root TaskNodes this bucket renders (space-filtered by the caller); a nested child is never also a root here. */
  nodes?: TaskNode[];
  collapsed?: ReadonlySet<number>;
  onToggleCollapse?: (taskId: number) => void;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
  /** Every space in the workspace, so each task row's "Move to space" submenu can list the others. */
  spaces: SpaceWithTasks[];
  onMoveTask: (task: Task, spaceId: number | null) => void;
  /** The ⋯ menu's items; absent for the synthetic "Ungrouped" bucket, which has nothing to add to or delete. */
  menuItems?: { label: string; icon: ReactNode; onSelect: () => void }[];
  /** Only set for a real Space (not the synthetic "Ungrouped" bucket, which has no Space to key on). */
  testId?: string;
  spaceId?: number;
  /** The id this bucket's open/closed state is persisted under (`space:<id>` / `ungrouped:<workspaceId>`). */
  nodeId: string;
}) {
  const [open, toggleOpen] = useNodeOpen(nodeId);
  const { attention, runStatus } = useRowSignal();
  const aggregate = aggregateStatus(tasks, attention, runStatus);

  const { active, rowProps, setMenuOpen } = useRowActive();

  return (
    <SidebarMenuSubItem>
      {/* Hover/focus scope: this header line only, not the li with the space's tasks (use-row-active.ts). */}
      <div className="sticky top-8 z-10 bg-sidebar" {...rowProps}>
        <SidebarMenuSubButton
          asChild
          className={cn("h-8 w-full translate-x-0 gap-2 rounded-lg", active && "pr-7")}
        >
          <button
            type="button"
            onClick={toggleOpen}
            aria-expanded={open}
            data-testid={testId}
            data-space-id={spaceId}
          >
            <Icons.space className={cn("size-3.5", active ? "text-foreground" : "text-foreground-subtle")} />
            <FadeText
              data-testid="container-title"
              fade={active}
              className={cn(
                "flex-initial text-ui-sm",
                active ? "text-foreground" : "text-foreground-subtle",
              )}
            >
              {title}
            </FadeText>
            <RowChevron open={open} />
            <AggregateSlot
              status={aggregate}
              label={`${title} activity`}
              className={active ? "invisible" : undefined}
            />
          </button>
        </SidebarMenuSubButton>
        {menuItems && (
          <RowActions>
            {active && (
              <RowMenu
                items={menuItems}
                onOpenChange={setMenuOpen}
                triggerProps={{
                  "data-testid": "sidebar-space-actions-trigger",
                  "data-space-id": spaceId ?? "",
                }}
              />
            )}
          </RowActions>
        )}
      </div>
      {open && (
        // One guide line under this header's icon; rows carry their own indent.
        <ContainerContent kind={spaceId != null ? "space" : "ungrouped"}>
          <TaskRows
            tasks={tasks}
            nodes={nodes}
            collapsed={collapsed}
            onToggleCollapse={onToggleCollapse}
            selectedTaskId={selectedTaskId}
            onSelectTask={onSelectTask}
            emptyText="No tasks"
            onArchiveTask={onArchiveTask}
            spaces={spaces}
            onMoveTask={onMoveTask}
          />
        </ContainerContent>
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
function AggregateSlot({ status, label, className }: { status: StatusDotStatus | null; label: string; className?: string }) {
  return (
    <span data-testid="row-aggregate-slot" className={cn("ml-auto flex w-2.5 shrink-0 items-center justify-center", className)}>
      {status && <StatusDot status={status} data-testid="row-aggregate" data-aggregate={status} aria-label={label} />}
    </span>
  );
}

/**
 * The task row's hover card (AC5): branch, diff stat and last activity --
 * the fields the single-line row no longer prints (branch, tier, lifecycle
 * status) plus the diff stat and task.UpdatedAt, no more.
 * Deliberately no PR state: smind's wire types (lib/types.ts's Task) carry
 * no PR field at all yet, and the plan's Decisions are explicit that this
 * card shows "only data the web UI already has; don't invent data."
 */
function TaskHoverCardBody({
  task,
  stat,
  status,
  tier,
  childCount,
  now,
}: {
  task: Task;
  stat?: TaskStat;
  status: string;
  tier: string | null;
  childCount: number;
  now: number;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <p className="truncate text-ui-base font-medium text-foreground">{task.Title}</p>
      {tier && (
        <p data-testid="task-tier" className="text-ui-sm text-muted-foreground">
          {tier}
          {childCount > 0 && ` · ${childCount} sub-task${childCount === 1 ? "" : "s"}`}
        </p>
      )}
      {(stat?.branch ?? task.Branch) && (
        <div className="flex items-center gap-1.5 text-ui-sm text-muted-foreground">
          <Icons.branch className="size-3 shrink-0" />
          <span data-testid="sidebar-task-branch" className="min-w-0 truncate">
            {stat?.branch ?? task.Branch}
          </span>
        </div>
      )}
      {stat && stat.filesChanged > 0 && (
        <p className="text-ui-sm tabular-nums text-muted-foreground">
          {stat.filesChanged} file{stat.filesChanged === 1 ? "" : "s"} changed,{" "}
          <span className="text-success">+{stat.insertions}</span>{" "}
          <span className="text-destructive">-{stat.deletions}</span>
        </p>
      )}
      <p className="text-ui-sm text-muted-foreground">
        Last activity <span data-testid="task-hover-card-last-activity">{formatRelativeTime(task.UpdatedAt, now)}</span>
      </p>
      <p data-testid="sidebar-task-status" className="text-ui-sm uppercase text-muted-foreground">
        {status}
      </p>
    </div>
  );
}

/**
 * Arrow-key navigation between task rows (the sidebar tree's keyboard
 * contract, same as a WAI-ARIA tree): Down/Up move to the next/previous
 * visible row, Left collapses an expanded parent (or jumps from a child to
 * its parent), Right expands a collapsed parent (or steps into its first
 * child). Only rows that are in the DOM count as visible: a collapsed
 * parent's descendants are not rendered. Returns whether the key was handled.
 */
function handleTaskRowArrowKey(
  key: string,
  row: HTMLElement,
  parent: { hasChildren: boolean; isCollapsed: boolean; onToggle: () => void },
): boolean {
  if (!key.startsWith("Arrow")) return false;
  const rows = Array.from(
    row.closest('[data-sidebar="menu"]')?.querySelectorAll<HTMLElement>('[data-testid="sidebar-task-row"]') ?? [],
  );
  const index = rows.indexOf(row);
  if (index < 0) return false;
  const focusRow = (target: HTMLElement | undefined): boolean => {
    if (!target) return false;
    target.focus();
    return true;
  };
  switch (key) {
    case "ArrowDown":
      return focusRow(rows[index + 1]) || true;
    case "ArrowUp":
      return focusRow(rows[index - 1]) || true;
    case "ArrowLeft": {
      if (parent.hasChildren && !parent.isCollapsed) {
        parent.onToggle();
        return true;
      }
      const parentId = row.getAttribute("data-parent-task-id");
      return parentId ? focusRow(rows.find((r) => r.getAttribute("data-task-id") === parentId)) : false;
    }
    case "ArrowRight": {
      if (!parent.hasChildren) return false;
      if (parent.isCollapsed) {
        parent.onToggle();
        return true;
      }
      return focusRow(rows[index + 1]);
    }
    default:
      return false;
  }
}

/**
 * The task row itself. Memoized so that one task's attention/run status
 * changing (a new RowSignal context value) re-renders only TaskRows -- the
 * parent components don't depend on it -- and the per-row work below is
 * skipped for rows whose props are all referentially stable: each task's
 * entry in the maps comes from one parent memo, so an unrelated task's
 * change produces the same `undefined`/value this row saw last time.
 * `tier`/`childCount` come from the forest (flat lists have none, and so
 * skip the chip and the tier).
 */
const TaskRow = memo(function TaskRow({
  task,
  tier,
  childCount,
  padLeft,
  isCollapsed,
  onToggleCollapse,
  rollup,
  selectedTaskId,
  onSelectTask,
  onArchiveTask,
  spaces,
  onMoveTask,
}: {
  task: Task;
  /** "supervisor" | "leader" | "coder"; absent for an untiered root. Shown in the hover card and the row's aria-label, not on the row. */
  tier: string | null;
  /** Direct children; > 0 makes this row a parent (chevron + child-count chip). */
  childCount: number;
  /** The row's own left padding in px (see ROW_INSET_PX): where its status slot starts. */
  padLeft: number;
  isCollapsed: boolean;
  onToggleCollapse?: (taskId: number) => void;
  /** The collapsed-parent attention rollup (PR-U1), or null when there is nothing to show. */
  rollup: { reason: AttentionReason; count: number } | null;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  onArchiveTask: (task: Task) => void;
  spaces?: SpaceWithTasks[];
  onMoveTask?: (task: Task, spaceId: number | null) => void;
}) {
  const { attention, statusOverrides, runStatus, stats, unread, onMarkUnread, pinned, onTogglePin, now } = useRowSignal();
  const hasChildren = childCount > 0;
  const reason = primaryAttentionReason(attention?.get(task.ID));
  const runState = runStatus.get(task.ID);
  const runDot = runDotStatus(runState);
  const stat = stats.get(task.ID);
  const isUnread = unread.has(task.ID);
  const slotSignal = taskSlotSignal(reason, runState, isUnread);
  const isTaskPinned = pinned.has(task.ID);
  const isSelected = task.ID === selectedTaskId;
  const status = statusOverrides.get(task.ID) ?? task.Status;
  const branch = stat?.branch ?? task.Branch;
  // Hover/focus/menu-open state lives on a wrapper around THIS row only. The
  // caller's li also contains this task's nested children list, so a CSS
  // group on the li would reveal every descendant's actions on one hover
  // (see use-row-active.ts).
  const { active, rowProps, setMenuOpen: setRowMenuOpen } = useRowActive();
  // The hover card must not sit on top of the row's menu (it hid the menu
  // entries' reasons): it is controlled and held shut while a menu is open.
  const [hoverOpen, setHoverOpen] = useState(false);
  const [menuOpen, setMenuOpenState] = useState(false);
  const setMenuOpen = (open: boolean) => {
    setRowMenuOpen(open);
    setMenuOpenState(open);
    // Forget a card that was open or pending when the menu opened, or it
    // would pop back up the moment the menu closes.
    if (open) setHoverOpen(false);
  };
  // On a narrow sidebar the ⋯ is overlaid on the row's right end (the time
  // slot is dropped there), where the child-count chip, rollup and setup badge
  // sit -- hide them while it shows rather than draw it over them.
  const hideForActions = active && "@max-[14rem]/sb:invisible";
  const treeActions = useContext(TreeActionsContext);
  const treeAvailability = treeActions?.availability.get(task.ID);
  const menuEntries = taskMenuEntries(task, {
    spaces,
    onMoveTask,
    isPinned: isTaskPinned,
    onTogglePin,
    onMarkUnread,
    onArchiveTask,
    tree: treeActions && treeAvailability
      ? {
          availability: treeAvailability,
          onArchiveFinished: () => treeActions.onArchiveFinished(task),
          onStopTree: () => treeActions.onStopTree(task),
        }
      : undefined,
  });
  const onRowKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
    if (handleTaskRowArrowKey(e.key, e.currentTarget, { hasChildren, isCollapsed, onToggle: () => onToggleCollapse?.(task.ID) })) {
      e.preventDefault();
    }
  };
  // The row shows only title + status + time, so everything it no longer
  // prints (tier, branch, what the dot means) is spoken here instead.
  const rowLabel = [
    task.Title,
    tier && (hasChildren ? `${tier} with ${childCount} sub-task${childCount === 1 ? "" : "s"}` : tier),
    branch && `branch ${branch}`,
    slotSignal.kind === "attention" && `needs attention: ${ATTENTION_LABEL[slotSignal.reason]}`,
    slotSignal.kind === "run" && `latest run ${slotSignal.status}`,
    slotSignal.kind === "unread" && "unread",
  ]
    .filter(Boolean)
    .join(", ");
  return (
    <ContextMenu onOpenChange={setMenuOpen}>
      <ContextMenuTrigger asChild>
        <div
          data-testid="sidebar-task-item"
          data-task-id={task.ID}
          className={cn(
            "relative flex h-8 min-w-0 items-center gap-1 rounded-lg pr-1 ring-sidebar-ring has-focus-visible:ring-2 has-focus-visible:ring-inset",
            isSelected ? "bg-selected" : active && "bg-hover",
          )}
          style={{ paddingLeft: padLeft }}
          {...rowProps}
        >
          {/*
           * Collapse chevron (PR-U1), on rows that have children, positioned
           * in the gutter left of the status slot (see ROW_GUTTER_PX). A
           * sibling of the row's select button, not a child: a button inside
           * a button is invalid HTML and breaks keyboard focus order.
           */}
          {hasChildren && (
            <button
              type="button"
              aria-label={(isCollapsed ? "Expand " : "Collapse ") + task.Title}
              aria-expanded={!isCollapsed}
              data-testid="task-collapse-toggle"
              onClick={() => onToggleCollapse?.(task.ID)}
              style={{ left: padLeft - ROW_GUTTER_PX }}
              className="absolute inset-y-0 flex w-3 items-center justify-center rounded-sm text-muted-foreground outline-hidden"
            >
              <Icons.chevronRight className={cn("size-3 transition-transform", !isCollapsed && "rotate-90")} />
            </button>
          )}
          <HoverCard openDelay={400} closeDelay={100} open={hoverOpen && !menuOpen} onOpenChange={(o) => setHoverOpen(o && !menuOpen)}>
            <HoverCardTrigger asChild>
              <SidebarMenuSubButton
                asChild
                // The highlight lives on the wrapper (so the chevron sits inside
                // it too), not on the select button.
                className="h-8 min-w-0 flex-1 translate-x-0 gap-1.5 rounded-lg px-1 text-left focus-visible:ring-0 hover:bg-transparent active:bg-transparent data-active:bg-transparent"
                isActive={isSelected}
              >
                <button
                  type="button"
                  aria-label={rowLabel}
                  aria-current={isSelected ? "true" : undefined}
                  onClick={() => onSelectTask?.(task)}
                  onKeyDown={onRowKeyDown}
                  data-testid="sidebar-task-row"
                  data-task-id={task.ID}
                  data-parent-task-id={task.ParentTaskID ?? undefined}
                >
                  {/*
                   * The ONE leading status slot (sidebar-polish S3): always in the
                   * DOM at a constant width -- a signal arriving or clearing must
                   * not move the title (refs/paseo/docs/design.md §11) -- and it
                   * draws a single signal by priority (see taskSlotSignal). The
                   * attention reason, latest run status and unread state stay
                   * queryable on the slot itself even when a higher-priority
                   * signal is the one drawn. The reason is in the accessible name
                   * as well as the colour.
                   */}
                  <span
                    data-testid="task-status-slot"
                    data-slot-signal={slotSignal.kind}
                    data-attention-reason={reason ?? undefined}
                    data-run-status={runState}
                    data-unread={isUnread ? "true" : undefined}
                    className="flex w-2 shrink-0 items-center justify-center"
                  >
                    {slotSignal.kind === "attention" && (
                      <StatusDot
                        status={attentionDotStatus(slotSignal.reason)}
                        data-testid="task-attention"
                        data-attention-reason={slotSignal.reason}
                        aria-label={`task needs attention: ${ATTENTION_LABEL[slotSignal.reason]}`}
                      />
                    )}
                    {slotSignal.kind === "run" && runDot && (
                      <StatusDot
                        status={runDot}
                        data-testid="task-run-status"
                        data-run-status={runState}
                        aria-label={`latest run ${runState}`}
                      />
                    )}
                    {slotSignal.kind === "unread" && (
                      <span
                        data-testid="task-unread-marker"
                        aria-label="unread"
                        className="size-1.5 shrink-0 rounded-full bg-primary"
                      />
                    )}
                  </span>
                  {/* min-w-0 flex-1: the title is the only thing on the line that may shrink. */}
                  <FadeText
                    data-testid="task-title"
                    fade={active}
                    className={cn(
                      "text-ui-base opacity-[.76] transition-opacity",
                      (active || isSelected) && "opacity-100",
                      active && "@max-[14rem]/sb:pr-6",
                    )}
                  >
                    {task.Title}
                  </FadeText>
                  {hasChildren && (
                    <span
                      data-testid="task-child-count"
                      aria-label={`${childCount} sub-task${childCount === 1 ? "" : "s"}`}
                      className={cn("shrink-0 rounded-full bg-tag/50 px-1.5 text-ui-xs tabular-nums text-foreground-subtle", hideForActions)}
                    >
                      {childCount}
                    </span>
                  )}
                  {/*
                   * Collapsed-parent attention rollup (PR-U1): the loudest
                   * reason among the hidden descendants plus how many need
                   * attention. Expanded parents show nothing here -- their
                   * children carry their own dots.
                   */}
                  {rollup && (
                    <span
                      data-testid="task-rollup"
                      data-rollup-reason={rollup.reason}
                      aria-label={`${rollup.count} ${rollup.count === 1 ? "descendant needs" : "descendants need"} attention: ${ATTENTION_LABEL[rollup.reason]}`}
                      className={cn("flex shrink-0 items-center gap-1", hideForActions)}
                    >
                      <StatusDot status={attentionDotStatus(rollup.reason)} />
                      <span className="text-ui-xs tabular-nums text-muted-foreground">{rollup.count}</span>
                    </span>
                  )}
                  <SetupBadge status={task.SetupStatus} compact className={cn(hideForActions) || undefined} />
                  {/* Dropped on a narrow sidebar (container query on the list) so the title keeps its width. */}
                  {stat && stat.filesChanged > 0 && (
                    <span
                      data-testid="sidebar-task-diffstat"
                      className="shrink-0 text-ui-xs tabular-nums @max-[14rem]/sb:hidden"
                      title={`${stat.filesChanged} changed, +${stat.insertions} -${stat.deletions}`}
                    >
                      <span className="text-success">+{stat.insertions}</span>{" "}
                      <span className="text-destructive">-{stat.deletions}</span>
                    </span>
                  )}
                  {/*
                   * Compact relative time. Its slot is a fixed width and is only
                   * made invisible (not removed) while the row's ⋯ is showing, so
                   * hovering never reflows the title; the ⋯ is overlaid on this
                   * same spot (ZCode TaskListItem's time -> actions swap).
                   */}
                  <span
                    data-testid="task-row-time"
                    className={cn(
                      "w-6 shrink-0 text-right text-ui-xs tabular-nums text-foreground-subtlest @max-[14rem]/sb:hidden",
                      active && "invisible",
                    )}
                  >
                    {formatRelativeTimeShort(task.UpdatedAt, now)}
                  </span>
                </button>
              </SidebarMenuSubButton>
            </HoverCardTrigger>
            <HoverCardContent data-testid="task-hover-card" side="right" align="start">
              <TaskHoverCardBody task={task} stat={stat} status={status} tier={tier} childCount={childCount} now={now} />
            </HoverCardContent>
          </HoverCard>
          {active && (
            <span className="absolute inset-y-0 right-1 flex items-center">
              <DropdownMenu onOpenChange={setMenuOpen}>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Actions for ${task.Title}`}
                    data-testid="sidebar-task-actions-trigger"
                    data-task-id={task.ID}
                    className="size-5 p-0"
                  >
                    <Icons.more />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <TaskMenuItems entries={menuEntries} parts={DROPDOWN_PARTS} />
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          )}
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent data-testid="sidebar-task-context-menu">
        <TaskMenuItems entries={menuEntries} parts={CONTEXT_PARTS} />
      </ContextMenuContent>
    </ContextMenu>
  );
});

/**
 * Renders a bucket's task rows. In the tree view (`nodes` given, PR-U1)
 * rows come from the pre-nested forest: each parent row owns a chevron
 * and its children render in a nested list below it -- a flush
 * SidebarMenuSub (plain ul/li wrappers, so recursion handles any depth
 * the daemon allows); each child row pads itself one ROW_INDENT_PX deeper
 * than its parent. Without `nodes` -- SearchResults, PinnedSection,
 * StatusGroupItem -- the list is exactly today's flat rendering; those
 * lists have no forest, so the tier chip is skipped there rather than
 * guessed. Root rows sit at ROOT_PAD_PX, or ROOT_PAD_WITH_PARENT_PX when
 * some root in the list has children.
 */
function TaskRows({
  tasks,
  nodes,
  collapsed,
  onToggleCollapse,
  selectedTaskId,
  onSelectTask,
  emptyText,
  onArchiveTask,
  spaces,
  onMoveTask,
}: {
  tasks: Task[];
  /** Root TaskNodes for the tree view; absent for the flat lists (search/pinned/status). */
  nodes?: TaskNode[];
  collapsed?: ReadonlySet<number>;
  onToggleCollapse?: (taskId: number) => void;
  selectedTaskId: number | null;
  onSelectTask?: (task: Task) => void;
  emptyText: string;
  onArchiveTask: (task: Task) => void;
  /**
   * Every space in the task's workspace, and the handler to reassign one --
   * both optional because SearchResults' flat list spans more than one
   * workspace and doesn't (yet) offer the move action. When present, each
   * row's action menu gains a "Move to space" submenu listing every space
   * here other than the task's own, plus "Ungrouped" unless the task is
   * already there.
   */
  spaces?: SpaceWithTasks[];
  onMoveTask?: (task: Task, spaceId: number | null) => void;
}) {
  const { attention, runStatus } = useRowSignal();

  if (tasks.length === 0) {
    return (
      <SidebarMenuSubItem>
        <span className="px-2 text-ui-sm text-muted-foreground">{emptyText}</span>
      </SidebarMenuSubItem>
    );
  }

  // Flat lists have no forest to derive a tier from; render today's rows.
  if (!nodes) {
    return (
      <>
        {tasks.map((task) => (
          <SidebarMenuSubItem key={task.ID}>
            <TaskRow
              task={task}
              tier={null}
              childCount={0}
              padLeft={ROOT_PAD_PX}
              isCollapsed={false}
              rollup={null}
              selectedTaskId={selectedTaskId}
              onSelectTask={onSelectTask}
              onArchiveTask={onArchiveTask}
              spaces={spaces}
              onMoveTask={onMoveTask}
            />
          </SidebarMenuSubItem>
        ))}
      </>
    );
  }

  // The chevron gutter is reserved only when some root in this list has
  // children (see ROW_GUTTER_PX); a flat list gives its titles that width.
  const rootPad = nodes.some((n) => n.children.length > 0) ? ROOT_PAD_WITH_PARENT_PX : ROOT_PAD_PX;

  const renderNode = (node: TaskNode, padLeft: number): ReactNode => {
    const { task, tier, children } = node;
    const isCollapsed = collapsed?.has(task.ID) ?? false;
    // Rollups only exist where children are hidden -- an expanded parent's
    // descendants show their own dots, and computing the walk for every
    // parent regardless would be wasted work on every attention change.
    const rollup =
      children.length > 0 && isCollapsed ? attentionRollup(node, attention, runStatus) : { reason: null, count: 0 };
    return (
      <SidebarMenuSubItem key={task.ID}>
        <TaskRow
          task={task}
          tier={tier}
          childCount={children.length}
          padLeft={padLeft}
          isCollapsed={isCollapsed}
          onToggleCollapse={onToggleCollapse}
          rollup={rollup.reason ? { reason: rollup.reason, count: rollup.count } : null}
          selectedTaskId={selectedTaskId}
          onSelectTask={onSelectTask}
          onArchiveTask={onArchiveTask}
          spaces={spaces}
          onMoveTask={onMoveTask}
        />
        {children.length > 0 && !isCollapsed && (
          // A real nested list (ul > li > ul, so the tree's structure is in
          // the DOM) that adds no indent of its own: each child row pads
          // itself one indent deeper than its parent.
          <SidebarMenuSub className={FLUSH_SUB}>
            {children.map((child) => renderNode(child, padLeft + ROW_INDENT_PX))}
          </SidebarMenuSub>
        )}
      </SidebarMenuSubItem>
    );
  };

  return <>{nodes.map((node) => renderNode(node, rootPad))}</>;
}

/**
 * One task's menu, defined once and rendered by BOTH the ⋯ dropdown and the
 * right-click context menu (sidebar-polish S6) so the two can never offer
 * different items or call different handlers -- the same approach
 * WorkspaceItem already takes with its `items`.
 */
type TaskMenuEntry =
  | { kind: "move"; targets: { id: number | null; label: string }[]; onMove: (spaceId: number | null) => void }
  | { kind: "item"; testId: string; label: string; icon: ReactNode; onSelect: () => void; disabled?: boolean; mutedNote?: string };

/**
 * "Move to space" lists every other space in the task's workspace plus
 * "Ungrouped" unless the task is already there (no self-move). It is omitted
 * with no `spaces`/`onMoveTask` pair (SearchResults' flat list spans workspaces
 * and doesn't offer the move) or nowhere to move to.
 */
function taskMenuEntries(
  task: Task,
  o: {
    spaces?: SpaceWithTasks[];
    onMoveTask?: (task: Task, spaceId: number | null) => void;
    isPinned: boolean;
    onTogglePin: (taskId: number) => void;
    onMarkUnread: (taskId: number) => void;
    onArchiveTask: (task: Task) => void;
    /** PR-H11 tree actions; present only on rows in the tree view whose task has live descendants. */
    tree?: { availability: TreeActionAvailability; onArchiveFinished: () => void; onStopTree: () => void };
  },
): TaskMenuEntry[] {
  const entries: TaskMenuEntry[] = [];
  if (o.tree) {
    const { availability, onArchiveFinished, onStopTree } = o.tree;
    entries.push(
      {
        kind: "item",
        testId: "sidebar-task-archive-finished-action",
        label: "Archive finished sub-tasks…",
        icon: <Icons.archive />,
        onSelect: onArchiveFinished,
        disabled: !availability.archive.enabled,
        mutedNote: availability.archive.reason ?? undefined,
      },
      {
        kind: "item",
        testId: "sidebar-task-stop-tree-action",
        label: "Stop all in tree…",
        icon: <Icons.stop />,
        onSelect: onStopTree,
        disabled: !availability.stop.enabled,
        mutedNote: availability.stop.reason ?? undefined,
      },
    );
  }
  if (o.spaces && o.onMoveTask) {
    const targets: { id: number | null; label: string }[] = [];
    if (task.SpaceID !== null) targets.push({ id: null, label: "Ungrouped" });
    for (const space of o.spaces) {
      if (space.ID !== task.SpaceID) targets.push({ id: space.ID, label: space.Title });
    }
    const onMoveTask = o.onMoveTask;
    if (targets.length > 0) entries.push({ kind: "move", targets, onMove: (spaceId) => onMoveTask(task, spaceId) });
  }
  entries.push(
    {
      kind: "item",
      testId: "sidebar-task-pin-action",
      label: o.isPinned ? "Unpin" : "Pin",
      icon: o.isPinned ? <Icons.unpin /> : <Icons.pin />,
      onSelect: () => o.onTogglePin(task.ID),
    },
    {
      kind: "item",
      testId: "sidebar-task-mark-unread-action",
      label: "Mark unread",
      icon: <Icons.statusIdle />,
      onSelect: () => o.onMarkUnread(task.ID),
    },
    {
      kind: "item",
      testId: "sidebar-task-archive-action",
      label: "Archive task",
      icon: <Icons.archive />,
      onSelect: () => o.onArchiveTask(task),
    },
  );
  return entries;
}

/** The Radix wrappers one menu flavour renders with; dropdown and context menus share every prop used below. */
interface MenuParts {
  Item: ComponentType<{ onSelect?: () => void; children?: ReactNode; disabled?: boolean; "data-testid"?: string; "data-space-id"?: string | number }>;
  Sub: ComponentType<{ children?: ReactNode }>;
  SubTrigger: ComponentType<{ children?: ReactNode; "data-testid"?: string }>;
  SubContent: ComponentType<{ children?: ReactNode }>;
}

const DROPDOWN_PARTS: MenuParts = {
  Item: DropdownMenuItem,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
};

const CONTEXT_PARTS: MenuParts = {
  Item: ContextMenuItem,
  Sub: ContextMenuSub,
  SubTrigger: ContextMenuSubTrigger,
  SubContent: ContextMenuSubContent,
};

function TaskMenuItems({ entries, parts }: { entries: TaskMenuEntry[]; parts: MenuParts }) {
  const { Item, Sub, SubTrigger, SubContent } = parts;
  return (
    <>
      {entries.map((entry) =>
        entry.kind === "move" ? (
          <Sub key="move">
            <SubTrigger data-testid="sidebar-task-move-action">
              <Icons.space /> Move to space
            </SubTrigger>
            <SubContent>
              {entry.targets.map((target) => (
                <Item
                  key={target.id ?? "ungrouped"}
                  data-testid="sidebar-task-move-target"
                  data-space-id={target.id ?? "ungrouped"}
                  onSelect={() => entry.onMove(target.id)}
                >
                  {target.label}
                </Item>
              ))}
            </SubContent>
          </Sub>
        ) : (
          <Item key={entry.testId} data-testid={entry.testId} onSelect={entry.onSelect} disabled={entry.disabled}>
            {entry.icon} {entry.label}
            {entry.mutedNote && <span className="ml-auto pl-2 text-ui-xs text-muted-foreground">{entry.mutedNote}</span>}
          </Item>
        ),
      )}
    </>
  );
}
