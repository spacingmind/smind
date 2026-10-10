import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS, isDesktop } from "@/lib/platform";
import { headerLeftInsetPx } from "@/lib/window-chrome";

/** Tailwind `p-2`, which an inline `paddingLeft` replaces. */
const BASE_PADDING_PX = 8;

/**
 * The app's top header row. In the desktop build it is also the window's
 * title bar: its empty area drags the window (and double-click maximizes),
 * and it pads clear of the macOS traffic lights or ends in the drawn caption
 * buttons on Windows/Linux. In a browser build it is the plain row it always
 * was.
 *
 * `sidebarPx` is how much width the sidebar takes up to the header's left
 * (see `headerLeftInsetPx`).
 */
export function AppHeader({ statusText, sidebarPx }: { statusText: string; sidebarPx: number }) {
  const { fullscreen } = useWindowState();
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, sidebarPx) : 0;

  return (
    // h-12/border-b/p-2 matches ZCode's WorkspaceHeader.tsx (zcode-visual-parity plan P2).
    // `deep`: every non-interactive descendant drags too; Tauri's drag script
    // already refuses to start a drag from buttons, inputs, links and
    // role=tab/menuitem elements, so none of those carry the attribute.
    <header
      data-testid="app-header"
      data-tauri-drag-region={isDesktop ? "deep" : undefined}
      className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
      style={leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined}
    >
      <SidebarTrigger />
      <Separator orientation="vertical" className="h-4" />
      <span className="text-ui-base text-muted-foreground" data-testid="app-connection-status">
        {statusText}
      </span>
      <div className="ml-auto" />
      <DesktopWindowControls />
    </header>
  );
}
