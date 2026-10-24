import { useCallback, useEffect, useMemo } from "react";

import { useCollapsedNodesStore } from "@/stores/collapsed-nodes-store";

export interface CollapsedNodes {
  /** The collapsed container ids (see readCollapsedNodes for the id scheme). */
  collapsed: ReadonlySet<string>;
  /** `defaultOpen: false` is for containers that start collapsed; an explicit expand is stored as `expanded:<id>`. */
  isOpen: (id: string, defaultOpen?: boolean) => boolean;
  toggle: (id: string, defaultOpen?: boolean) => void;
  /** Force a container open, e.g. a just-created workspace whose id may still be persisted as collapsed from a deleted one. */
  open: (id: string) => void;
}

/** Ids that name a row of the fetched tree and so can go stale; `pinned` and `status:*` are fixed and never pruned. */
const PRUNABLE = /^(ws|space|ungrouped):/;

/** Marker for a default-collapsed container the user expanded; the set stores collapses, so this is the one stored expand. */
const EXPANDED_PREFIX = "expanded:";

/**
 * The sidebar's persisted expand/collapse state for workspaces, spaces, the
 * Pinned section and status groups (sidebar-polish S8), the sibling of
 * useCollapsedTasks for the task tree, from the shared preference store so
 * every reader sees collapses change live.
 *
 * `liveNodeIds` prunes ids of deleted workspaces/spaces with the same
 * `null`-means-not-loaded-yet contract as useCollapsedTasks, so the empty
 * first render cannot wipe the persisted set before the real tree lands -- and
 * so a deleted workspace's id, if the database ever reuses it, comes back open.
 */
export function useCollapsedNodes(liveNodeIds: ReadonlySet<string> | null): CollapsedNodes {
  const collapsed = useCollapsedNodesStore((s) => s.value);

  useEffect(() => {
    if (liveNodeIds === null) return;
    const { value: prev, set } = useCollapsedNodesStore.getState();
    let changed = false;
    const next = new Set<string>();
    for (const id of prev) {
      if (!PRUNABLE.test(id) || liveNodeIds.has(id)) next.add(id);
      else changed = true;
    }
    if (changed) set(next);
  }, [liveNodeIds]);

  const isOpen = useCallback(
    (id: string, defaultOpen = true) => (defaultOpen ? !collapsed.has(id) : collapsed.has(EXPANDED_PREFIX + id)),
    [collapsed],
  );

  const toggle = useCallback((id: string, defaultOpen = true) => {
    const { value: prev, set } = useCollapsedNodesStore.getState();
    const next = new Set(prev);
    const key = defaultOpen ? id : EXPANDED_PREFIX + id;
    if (!next.delete(key)) next.add(key);
    set(next);
  }, []);

  const open = useCallback((id: string) => {
    const { value: prev, set } = useCollapsedNodesStore.getState();
    if (!prev.has(id)) return;
    const next = new Set(prev);
    next.delete(id);
    set(next);
  }, []);

  return useMemo(() => ({ collapsed, isOpen, toggle, open }), [collapsed, isOpen, toggle, open]);
}
