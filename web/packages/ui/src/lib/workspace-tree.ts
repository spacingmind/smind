import type { Space, Task, Workspace } from "@/lib/types";

/** A space plus the subset of its workspace's tasks scoped to it (Task.SpaceID === Space.ID). */
export interface SpaceWithTasks extends Space {
  tasks: Task[];
}

/** One workspace with its spaces (each carrying its own tasks) and its space-less tasks. */
export interface WorkspaceWithTree extends Workspace {
  spaces: SpaceWithTasks[];
  /** Tasks with SpaceID === null -- always present here even when spaces.length > 0, never dropped. */
  ungroupedTasks: Task[];
}

/**
 * The ADR 0009 lifecycle topics the sidebar subscribes to, plus the three
 * ADR 0005 originals. `event.dropped` is deliberately absent: it is a
 * *synthetic* topic the daemon's pump injects on queue overflow, not one
 * `events.subscribe` accepts (`knownTopics` in internal/wsapi/events.go
 * would reject it), so consumers listen for it locally without asking the
 * daemon for it.
 */
export const LIFECYCLE_TOPICS = [
  "workspace.created",
  "workspace.deleted",
  "space.created",
  "space.deleted",
  "task.created",
  "task.updated",
  "task.archived",
  "task.deleted",
] as const;

/** Groups a workspace's flat task.list by Task.SpaceID into the shape the sidebar renders. */
export function buildWorkspaceTree(workspace: Workspace, spaces: Space[], tasks: Task[]): WorkspaceWithTree {
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
    ...workspace,
    spaces: spaces.map((sp) => ({ ...sp, tasks: tasksBySpaceId.get(sp.ID) ?? [] })),
    ungroupedTasks,
  };
}

/**
 * Inserts into a list ordered by ascending ID, replacing an existing entry
 * with the same ID rather than appending a duplicate. Every `*.list` RPC
 * behind this tree is `ORDER BY id` (internal/store), so ID order is what
 * a refetch would produce -- an event-spliced row therefore lands exactly
 * where the next reconnect's refetch will put it, and the acting client
 * (which also calls refresh() after its own mutation) can never
 * double-insert, whichever of the two arrives first. ADR 0009 requires
 * both properties: "clients must treat an insert as an upsert keyed on
 * ID".
 */
function upsertById<T extends { ID: number }>(list: T[], entity: T): T[] {
  const existing = list.findIndex((e) => e.ID === entity.ID);
  if (existing !== -1) {
    const next = list.slice();
    next[existing] = entity;
    return next;
  }
  const at = list.findIndex((e) => e.ID > entity.ID);
  if (at === -1) return [...list, entity];
  return [...list.slice(0, at), entity, ...list.slice(at)];
}

/** True when `payload` is a non-null object we can read named fields off. */
function isRecord(payload: unknown): payload is Record<string, unknown> {
  return typeof payload === "object" && payload !== null;
}

/** Reads `payload.<key>` as an entity with a numeric ID, or null if the payload isn't that shape. */
function entityField<T extends { ID: number }>(payload: unknown, key: string): T | null {
  if (!isRecord(payload)) return null;
  const value = payload[key];
  if (!isRecord(value) || typeof value.ID !== "number") return null;
  return value as unknown as T;
}

/** Reads `payload.<key>` as a number, or null if absent/not a number. */
function numberField(payload: unknown, key: string): number | null {
  if (!isRecord(payload)) return null;
  const value = payload[key];
  return typeof value === "number" ? value : null;
}

/** Drops `taskId` from every bucket of every workspace. Returns `tree` unchanged if it wasn't anywhere. */
function removeTask(tree: WorkspaceWithTree[], taskId: number): WorkspaceWithTree[] {
  let changed = false;
  const next = tree.map((ws) => {
    const ungrouped = ws.ungroupedTasks.filter((t) => t.ID !== taskId);
    let spacesChanged = false;
    const spaces = ws.spaces.map((sp) => {
      const tasks = sp.tasks.filter((t) => t.ID !== taskId);
      if (tasks.length === sp.tasks.length) return sp;
      spacesChanged = true;
      return { ...sp, tasks };
    });
    if (ungrouped.length === ws.ungroupedTasks.length && !spacesChanged) return ws;
    changed = true;
    return { ...ws, spaces, ungroupedTasks: ungrouped };
  });
  return changed ? next : tree;
}

/**
 * Places `task` in the bucket its own WorkspaceID/SpaceID name, removing
 * it from wherever it currently sits first -- so `task.updated` doubles as
 * a move, even though no daemon operation re-parents a task today.
 *
 * Returns `tree` unchanged when the target bucket isn't loaded (an event
 * for a workspace this tree doesn't hold, or a space it hasn't fetched):
 * dropping the row would be worse than ignoring the event, since the next
 * reconnect refetch reconciles either way.
 */
function upsertTask(tree: WorkspaceWithTree[], task: Task): WorkspaceWithTree[] {
  const wsIndex = tree.findIndex((ws) => ws.ID === task.WorkspaceID);
  if (wsIndex === -1) return tree;
  if (task.SpaceID !== null && !tree[wsIndex].spaces.some((sp) => sp.ID === task.SpaceID)) return tree;

  const pruned = removeTask(tree, task.ID);
  const ws = pruned[wsIndex];
  const next = pruned.slice();
  next[wsIndex] =
    task.SpaceID === null
      ? { ...ws, ungroupedTasks: upsertById(ws.ungroupedTasks, task) }
      : {
          ...ws,
          spaces: ws.spaces.map((sp) => (sp.ID === task.SpaceID ? { ...sp, tasks: upsertById(sp.tasks, task) } : sp)),
        };
  return next;
}

/**
 * Folds one ADR 0009 lifecycle notification into the fetched tree,
 * returning the *same array reference* when the event changes nothing --
 * an event for a workspace this client doesn't hold, a delete of something
 * already gone, or a malformed payload. Callers pass the result straight
 * to a setState updater, so reference equality is what keeps an unrelated
 * tab's events from re-rendering the whole sidebar.
 *
 * Archive is treated as removal, matching `ListTasks` (which filters
 * archived rows out) and the tree a refetch would return -- ADR 0009
 * explicitly leaves that call to the client, since the row itself
 * survives and `task.get` still returns it.
 *
 * Container deletes prune the subtree locally: ADR 0009 publishes only the
 * root event for a cascade, never one per descendant.
 */
export function applyLifecycleEvent(
  tree: WorkspaceWithTree[],
  topic: string,
  payload: unknown,
): WorkspaceWithTree[] {
  switch (topic) {
    case "workspace.created": {
      const workspace = entityField<Workspace>(payload, "workspace");
      if (!workspace) return tree;
      // A just-created workspace has no spaces and no tasks yet; if this
      // client already holds it, upsertById refreshes the row's own fields
      // without touching the subtree it already fetched.
      const existing = tree.find((ws) => ws.ID === workspace.ID);
      return upsertById(tree, {
        ...workspace,
        spaces: existing?.spaces ?? [],
        ungroupedTasks: existing?.ungroupedTasks ?? [],
      });
    }

    case "workspace.deleted": {
      const id = numberField(payload, "id");
      if (id === null) return tree;
      const next = tree.filter((ws) => ws.ID !== id);
      return next.length === tree.length ? tree : next;
    }

    case "space.created": {
      const space = entityField<Space>(payload, "space");
      if (!space) return tree;
      const wsIndex = tree.findIndex((ws) => ws.ID === space.WorkspaceID);
      if (wsIndex === -1) return tree;
      const ws = tree[wsIndex];
      const existing = ws.spaces.find((sp) => sp.ID === space.ID);
      const next = tree.slice();
      next[wsIndex] = { ...ws, spaces: upsertById(ws.spaces, { ...space, tasks: existing?.tasks ?? [] }) };
      return next;
    }

    case "space.deleted": {
      const id = numberField(payload, "id");
      const workspaceId = numberField(payload, "workspaceId");
      if (id === null || workspaceId === null) return tree;
      const wsIndex = tree.findIndex((ws) => ws.ID === workspaceId);
      if (wsIndex === -1) return tree;
      const ws = tree[wsIndex];
      const spaces = ws.spaces.filter((sp) => sp.ID !== id);
      if (spaces.length === ws.spaces.length) return tree;
      const next = tree.slice();
      next[wsIndex] = { ...ws, spaces };
      return next;
    }

    case "task.created":
    case "task.updated": {
      const task = entityField<Task>(payload, "task");
      if (!task) return tree;
      // Defensive: a create/update carrying an already-archived row means
      // the same thing a task.archived does, and ListTasks would not
      // return it.
      if (task.ArchivedAt !== null) return removeTask(tree, task.ID);
      return upsertTask(tree, task);
    }

    case "task.archived": {
      const task = entityField<Task>(payload, "task");
      if (!task) return tree;
      return removeTask(tree, task.ID);
    }

    case "task.deleted": {
      const id = numberField(payload, "id");
      if (id === null) return tree;
      return removeTask(tree, id);
    }

    default:
      return tree;
  }
}

/** One hit from `searchTasks`: the task, plus the containers it was found under, so a flat row can still say where it lives. */
export interface TaskSearchResult {
  task: Task;
  workspace: WorkspaceWithTree;
  /** The space the task sits in, or null for an ungrouped task. */
  space: SpaceWithTasks | null;
}

/**
 * Flattens the tree to the tasks matching `query`, in tree order.
 *
 * Matching is a case-insensitive substring over the task's own title *and*
 * over the names of the containers it sits in -- following
 * `audit-deepseek-harness.md` §3, whose collapsed search matches "title
 * and workspace substrings". Typing a workspace or space name is therefore
 * a way to list everything under it, which is the thing a tree filter is
 * usually wanted for.
 *
 * A blank (or whitespace-only) query returns no results rather than every
 * task: the caller's rule is that a blank query means "not searching at
 * all", and it renders the tree instead.
 */
export function searchTasks(tree: readonly WorkspaceWithTree[], query: string): TaskSearchResult[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return [];

  const results: TaskSearchResult[] = [];
  const matches = (...haystack: (string | null | undefined)[]) =>
    haystack.some((h) => h != null && h.toLowerCase().includes(needle));

  for (const workspace of tree) {
    const workspaceMatches = matches(workspace.Title, workspace.Path);
    for (const space of workspace.spaces) {
      const containerMatches = workspaceMatches || matches(space.Title);
      for (const task of space.tasks) {
        if (containerMatches || matches(task.Title)) results.push({ task, workspace, space });
      }
    }
    for (const task of workspace.ungroupedTasks) {
      if (workspaceMatches || matches(task.Title)) results.push({ task, workspace, space: null });
    }
  }
  return results;
}
