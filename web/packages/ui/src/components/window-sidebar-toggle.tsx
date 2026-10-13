import { SidebarTrigger } from "@/components/ui/sidebar";
import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS } from "@/lib/platform";
import { toggleLeftPx } from "@/lib/window-chrome";

/**
 * The desktop shell's one sidebar toggle (ZCode's top overlay, Paseo's
 * window-level toggle): an overlay on the window's top-left, outside both the
 * sidebar and the header, at a fixed x -- right after the macOS traffic
 * lights, centred in the 48px rail elsewhere. Because it is mounted once, it
 * neither jumps nor remounts (so keyboard focus survives) when the sidebar
 * collapses/expands; the sidebar's top strip and the header pad around it
 * (`toggleZoneEndPx`). The wrapper is click-through so the window-drag strips
 * beneath it keep working; only the button takes pointer events.
 *
 * Render it inside the shell's `relative` row and inside `SidebarProvider`.
 */
export function WindowSidebarToggle() {
  const { fullscreen } = useWindowState();
  return (
    <div
      data-testid="window-sidebar-toggle"
      className="pointer-events-none absolute top-0 z-30 flex h-12 items-center transition-[left] duration-200 ease-out motion-reduce:transition-none"
      style={{ left: toggleLeftPx(desktopOS, fullscreen) }}
    >
      <SidebarTrigger className="pointer-events-auto text-foreground-muted hover:text-foreground" />
    </div>
  );
}
