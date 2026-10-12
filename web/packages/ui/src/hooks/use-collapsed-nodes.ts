import { useCallback, useEffect, useMemo, useState } from "react";

import { readCollapsedNodes, writeCollapsedNodes } from "@/lib/sidebar-preferences";

export interface CollapsedNodes {
  /** The collapsed container ids (see readCollapsedNodes for the id scheme). */
  collapsed: ReadonlySet<string>;
  isOpen: (id: string) => boolean;
  toggle: (id: string) => void;
  /** Force a container open, e.g. a just-created workspace whose id may still be persisted as collapsed from a deleted one. */
  open: (id: string) => void;
}

/** Ids that name a row of the fetched tree and so can go stale; `pinned` and `status:*` are fixed and never pruned. */
const PRUNABLE = /^(ws|space|ungrouped):/;

/**
 * The sidebar's persisted expand/collapse state for workspaces, spaces, the
 * Pinned section and status groups (sidebar-polish S8), the sibling of
 * useCollapsedTasks for the task tree. A per-mount useState is enough because
 * only AppSidebar's single live instance reads or writes it.
 *
 * `liveNodeIds` prunes ids of deleted workspaces/spaces with the same
 * `null`-means-not-loaded-yet contract as useCollapsedTasks, so the empty
 * first render cannot wipe the persisted set before the real tree lands -- and
 * so a deleted workspace's id, if the database ever reuses it, comes back open.
 */
export function useCollapsedNodes(liveNodeIds: ReadonlySet<string> | null): CollapsedNodes {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => readCollapsedNodes());

  useEffect(() => {
    writeCollapsedNodes(collapsed);
  }, [collapsed]);

  useEffect(() => {
    if (liveNodeIds === null) return;
    setCollapsed((prev) => {
      let changed = false;
      const next = new Set<string>();
      for (const id of prev) {
        if (!PRUNABLE.test(id) || liveNodeIds.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [liveNodeIds]);

  const isOpen = useCallback((id: string) => !collapsed.has(id), [collapsed]);

  const toggle = useCallback((id: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }, []);

  const open = useCallback((id: string) => {
    setCollapsed((prev) => {
      if (!prev.has(id)) return prev;
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, []);

  return useMemo(() => ({ collapsed, isOpen, toggle, open }), [collapsed, isOpen, toggle, open]);
}
