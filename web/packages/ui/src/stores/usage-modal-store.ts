import { create } from "zustand";

/**
 * Open state of the plan-usage modal (not persisted), as a shared store so any surface -- the command
 * palette's "Usage: open plan usage", the chat hover card's "Open Usage", the sidebar footer item -- can open
 * it without a prop chain through the shell. The modal itself is mounted once, by `UsageModalRoot`.
 */
export const useUsageModalStore = create<{ open: boolean; setOpen: (open: boolean) => void }>()((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
}));
