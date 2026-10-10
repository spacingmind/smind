import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS, isDesktop } from "@/lib/platform";
import { hasTrafficLights } from "@/lib/window-chrome";

/**
 * Top edge of the sidebar on macOS: an empty, draggable row the height of the
 * app header, where the native traffic lights sit. Present for the expanded
 * sidebar, the collapsed rail and the mobile sheet alike (it is part of the
 * sidebar's own content); absent in fullscreen, where macOS hides the lights,
 * and in every other build.
 */
export function DesktopSidebarInset() {
  const { fullscreen } = useWindowState();
  if (!isDesktop || !hasTrafficLights(desktopOS, fullscreen)) return null;
  return <div data-testid="sidebar-window-inset" data-tauri-drag-region className="h-12 shrink-0 border-b" />;
}
