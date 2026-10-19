import { queryOptions } from "@tanstack/react-query";

import { queryKeys } from "@/lib/query-keys";
import type { Space, Task, Workspace } from "@/lib/types";
import { buildWorkspaceTree, type WorkspaceWithTree } from "@/lib/workspace-tree";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * Loads workspace.list plus, per workspace, space.list and task.list (both scoped by workspaceId, fetched in
 * parallel). task.list returns every task in the workspace regardless of space (internal/wsapi's
 * handleTaskList), so grouping by Task.SpaceID happens client-side (`buildWorkspaceTree`). `?? []` tolerates
 * a daemon that answers null for an empty list.
 */
export async function fetchWorkspaceTree(client: WsClientLike): Promise<WorkspaceWithTree[]> {
  const list = (await client.call<Workspace[]>("workspace.list")) ?? [];
  return Promise.all(
    list.map(async (ws) => {
      const [spaces, tasks] = await Promise.all([
        client.call<Space[]>("space.list", { workspaceId: ws.ID }).then((r) => r ?? []),
        client.call<Task[]>("task.list", { workspaceId: ws.ID }).then((r) => r ?? []),
      ]);
      return buildWorkspaceTree(ws, spaces, tasks);
    }),
  );
}

/**
 * The shared workspace-tree query (ADR-0032): the sidebar and Settings -> Usage read this one entry. The
 * lifecycle events (ADR 0009) splice into it through the event bridge, `event.dropped` and a same-daemon
 * reconnect invalidate it, and a mutation's `refresh()` is an invalidation -- a background re-read that keeps
 * the tree on screen instead of blanking it.
 */
export function workspaceTreeQueryOptions(client: WsClientLike | null) {
  return queryOptions({
    queryKey: queryKeys.workspaceTree(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: () => {
      if (!client) throw new Error("no daemon connection");
      return fetchWorkspaceTree(client);
    },
  });
}
