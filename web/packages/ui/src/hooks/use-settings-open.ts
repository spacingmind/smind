import { useCallback, useState } from "react";

/**
 * The plain open/close state SettingsScreen's host renders from --
 * deliberately not tied to routing or a keyboard shortcut. Item 13 ships
 * only a sidebar entry point (see app-sidebar.tsx's settings button);
 * wiring a route and a shortcut to this same open/close surface is
 * Track A's (App.tsx owns routing -- see the plan's Tracks section).
 */
export function useSettingsOpen(initial = false): {
  open: boolean;
  setOpen: (open: boolean) => void;
  openSettings: () => void;
  closeSettings: () => void;
} {
  const [open, setOpen] = useState(initial);
  const openSettings = useCallback(() => setOpen(true), []);
  const closeSettings = useCallback(() => setOpen(false), []);
  return { open, setOpen, openSettings, closeSettings };
}
