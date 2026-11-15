import { createContext, useContext } from "react";

import { useWindowState } from "@/hooks/use-window-state";
import { headerLeftInsetPx } from "@/lib/window-chrome";
import { desktopOS, isDesktop } from "@/lib/platform";

/**
 * Where a full-page view (Settings, Inbox, Automations, a plugin page) is drawn. On desktop widths the shell keeps
 * its sidebar and the page fills the content region beside it, so the macOS traffic lights stay over the sidebar
 * header and the page needs no inset of its own. Below the mobile breakpoint, and in tests that mount a page on its
 * own, the page covers the whole window and clears the lights itself (the old behaviour).
 */
export const PageChromeContext = createContext<{ sidebarKept: boolean }>({ sidebarKept: false });

/** True when the page sits beside the app sidebar rather than over the whole window. */
export function usePageSidebarKept(): boolean {
  return useContext(PageChromeContext).sidebarKept;
}

/** The left padding a page header needs to clear the native traffic lights: none when the sidebar is kept. */
export function usePageLeftInset(): number {
  const { fullscreen } = useWindowState();
  const kept = usePageSidebarKept();
  return isDesktop && !kept ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;
}
