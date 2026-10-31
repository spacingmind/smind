import { useEffect, useRef } from "react";

import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { PluginPanelPane } from "@/components/plugin-panel/plugin-panel-pane";
import { Button } from "@/components/ui/button";
import { findPanel, usePluginPanelsQuery } from "@/hooks/use-plugin-panels";
import { useWindowState } from "@/hooks/use-window-state";
import { Icons } from "@/lib/icons";
import { desktopOS, isDesktop } from "@/lib/platform";
import { headerLeftInsetPx } from "@/lib/window-chrome";

const BASE_PADDING_PX = 8;

/**
 * A plugin panel as a full-page view (ADR-0042): the surface a
 * `where: "sidebar"` panel -- or any panel opened with no task selected --
 * lands on. Same full-screen overlay shape as the inbox, with the panel's
 * own contribution title and the same pane body a task's plugin tab
 * renders (no taskId: the page has no task context). If the daemon's panel
 * list no longer has the panel (the plugin was disabled while the page was
 * open), the page walks itself back.
 */
export function PluginPage({
  pluginId,
  panelId,
  onNavigateBack,
}: {
  pluginId: string;
  panelId: string;
  onNavigateBack: () => void;
}) {
  const panels = usePluginPanelsQuery();
  const panel = panels.data ? findPanel(panels.data.panels, pluginId, panelId) : undefined;
  const { fullscreen } = useWindowState();
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;
  const headerStyle = leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined;

  // Once only: a re-render while still gone must not navigate again.
  const navigatedBack = useRef(false);
  useEffect(() => {
    if (!panels.isSuccess || panel) return;
    if (navigatedBack.current) return;
    navigatedBack.current = true;
    onNavigateBack();
  }, [panels.isSuccess, panel, onNavigateBack]);

  return (
    <div className="flex h-full min-h-0 w-full flex-col bg-background" data-testid="plugin-page">
      <header
        className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
        data-testid="plugin-page-header"
        data-tauri-drag-region={isDesktop ? "deep" : undefined}
        style={headerStyle}
      >
        <Button variant="ghost" size="icon" aria-label="Back to app" data-testid="plugin-page-back" onClick={onNavigateBack}>
          <Icons.back className="size-4" />
        </Button>
        <h1 className="truncate text-ui-base font-medium" data-testid="plugin-page-title">
          {panel?.title ?? panelId}
        </h1>
        <div className="ml-auto" />
        <DesktopWindowControls />
      </header>
      <main className="min-h-0 flex-1" data-testid="plugin-page-content">
        <PluginPanelPane pluginId={pluginId} panelId={panelId} />
      </main>
    </div>
  );
}
