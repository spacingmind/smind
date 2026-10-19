import type { SidebarGroupMode } from "@/lib/sidebar-preferences";
import { useSidebarGroupModeStore } from "@/stores/sidebar-group-mode-store";

export type { SidebarGroupMode };

/** The sidebar's tree-vs-status view toggle (AC6), from the shared preference store. */
export function useSidebarGroupMode(): {
  groupMode: SidebarGroupMode;
  setGroupMode: (mode: SidebarGroupMode) => void;
} {
  const groupMode = useSidebarGroupModeStore((s) => s.value);
  const setGroupMode = useSidebarGroupModeStore((s) => s.set);
  return { groupMode, setGroupMode };
}
