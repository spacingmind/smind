import { useCallback, useState } from "react";

import { readSidebarGroupMode, writeSidebarGroupMode, type SidebarGroupMode } from "@/lib/sidebar-preferences";

export type { SidebarGroupMode };

/**
 * The sidebar's tree-vs-status view toggle (AC6), persisted in
 * localStorage. Same "plain per-mount useState, single live reader" shape
 * as use-pinned-tasks.ts -- only AppSidebar reads or writes this.
 */
export function useSidebarGroupMode(): {
  groupMode: SidebarGroupMode;
  setGroupMode: (mode: SidebarGroupMode) => void;
} {
  const [groupMode, setGroupModeState] = useState<SidebarGroupMode>(() => readSidebarGroupMode());

  const setGroupMode = useCallback((mode: SidebarGroupMode) => {
    setGroupModeState(mode);
    writeSidebarGroupMode(mode);
  }, []);

  return { groupMode, setGroupMode };
}
