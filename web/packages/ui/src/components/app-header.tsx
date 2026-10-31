import { Icons } from "@/lib/icons";
import type { ReactNode } from "react";

import { AppMenu } from "@/components/app-menu";
import { HeaderTaskMenu, HeaderTaskTitle } from "@/components/app-header-task";
import { TaskBudgetControl } from "@/components/task-budget-control";
import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { OpenInEditorButton } from "@/components/open-in-editor-button";
import { Button } from "@/components/ui/button";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { UpdateBadge } from "@/components/update-badge";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useLiveTask } from "@/hooks/use-live-task";
import { useLocalPathActions } from "@/hooks/use-local-path-actions";
import { useWindowState } from "@/hooks/use-window-state";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import type { AppUpdateState } from "@/lib/app-update";
import { desktopOS, isDesktop } from "@/lib/platform";
import type { ConnectionStatus } from "@/lib/reconnect";
import type { Task } from "@/lib/types";
import { cn } from "@/lib/utils";
import { HEADER_BASE_PADDING_PX, headerLeftInsetPx } from "@/lib/window-chrome";
import type { WsClient } from "@/lib/ws-client";

const ICON_BUTTON = "text-foreground-muted hover:text-foreground";

function HeaderIconButton({
  label,
  testId,
  onClick,
  className,
  children,
}: {
  label: string;
  testId: string;
  onClick: () => void;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={label}
          data-testid={testId}
          className={cn(ICON_BUTTON, className)}
          onClick={onClick}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

/** The selected task's title + ⋯ menu; follows the task's own update events so the hover card never shows a stale status. */
function HeaderTask({
  task,
  client,
  events,
  localPathActions,
}: {
  task: Task;
  client: WsClient | null;
  events: DaemonEvents | null;
  localPathActions: ReturnType<typeof useLocalPathActions>;
}) {
  const { task: live } = useLiveTask(task, events);
  return (
    <div data-testid="app-header-task" className="flex min-w-0 items-center gap-0.5">
      <HeaderTaskTitle task={live} />
      <HeaderTaskMenu task={live} client={client} localPathActions={localPathActions} />
      <TaskBudgetControl task={live} client={client} />
    </div>
  );
}

/**
 * The app's single top bar (ZCode's WorkspaceHeader): the first row of the
 * inset content panel. Left: the sidebar toggle + update badge when the
 * sidebar has no room for them (collapsed rail / compact sheet; expanded,
 * the sidebar's own top strip hosts them), then the task identity. Right: an
 * open-in-editor pill, help, terminal, and on Windows/Linux the app menu and
 * drawn caption buttons.
 *
 * In the desktop build it is also the window's title bar: its empty area
 * drags the window (and double-click maximizes) and it pads clear of the
 * macOS traffic lights. In a browser it is the plain row.
 *
 * `sidebarPx` is how much width the sidebar takes up to the header's left
 * (see `headerLeftInsetPx`).
 */
export function AppHeader({
  statusText,
  connectionStatus = "connected",
  sidebarPx,
  showSidebarToggle = true,
  showUpdateBadge = showSidebarToggle,
  windowToggle = false,
  task = null,
  client = null,
  events = null,
  onOpenTerminal,
  updateState = { stage: "idle" },
  onOpenUpdate,
}: {
  statusText: string;
  connectionStatus?: ConnectionStatus;
  sidebarPx: number;
  /** Whether the toggle (and, by default, the update badge) live here rather than in the sidebar's top strip or the window-level toggle. */
  showSidebarToggle?: boolean;
  /** Whether the update badge lives here (it is in the sidebar's strip while that is expanded). Defaults to `showSidebarToggle`. */
  showUpdateBadge?: boolean;
  /** The desktop shell's window-level sidebar toggle floats over this header's left edge (`WindowSidebarToggle`): clear its zone. */
  windowToggle?: boolean;
  task?: Task | null;
  client?: WsClient | null;
  events?: DaemonEvents | null;
  /** Opens (or focuses) the selected task's terminal tab. */
  onOpenTerminal?: () => void;
  updateState?: AppUpdateState;
  onOpenUpdate?: () => void;
}) {
  const { fullscreen } = useWindowState();
  const { runAction } = useKeyboard();
  const localPathActions = useLocalPathActions();
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, sidebarPx, windowToggle) : 0;
  const connected = connectionStatus === "connected";

  return (
    // Own provider: the header is mounted by tests (and the shell's own unit
    // mounts) without main.tsx's app-level one.
    <TooltipProvider>
      {/*
       * h-12/border-b/p-2 matches ZCode's WorkspaceHeader.tsx (zcode-visual-parity plan P2).
       * `deep`: every non-interactive descendant drags too; Tauri's drag script
       * already refuses to start a drag from buttons, inputs, links and
       * role=tab/menuitem elements, so none of those carry the attribute.
       * `@container`: the right cluster drops its secondary controls on narrow widths.
       */}
      <header
        data-testid="app-header"
        data-tauri-drag-region={isDesktop ? "deep" : undefined}
        className={cn(
          "@container/app-header flex h-12 shrink-0 items-center gap-2 border-b border-border/50 bg-panel p-2",
          // The inset follows the sidebar's own width animation (App.tsx's panel: 200ms ease-out), so the title slides with it instead of jumping.
          isDesktop && "transition-[padding-left] duration-200 ease-out motion-reduce:transition-none",
        )}
        style={leftInset > 0 ? { paddingLeft: HEADER_BASE_PADDING_PX + leftInset } : undefined}
      >
        {showSidebarToggle && <SidebarTrigger className={cn(ICON_BUTTON, "max-sm:min-h-9 max-sm:min-w-9")} />}
        {showUpdateBadge && onOpenUpdate && <UpdateBadge state={updateState} onOpen={onOpenUpdate} />}
        {task && <HeaderTask task={task} client={client} events={events} localPathActions={localPathActions} />}
        {/* Quiet while connected (sr-only keeps the live status for assistive tech); a pill only when something is wrong. */}
        <span
          role="status"
          data-testid="app-connection-status"
          data-connection={connectionStatus}
          className={cn(
            connected
              ? "sr-only"
              : cn(
                  "shrink-0 rounded-full px-2 py-0.5 text-ui-sm",
                  connectionStatus === "disconnected" ? "bg-destructive/10 text-destructive" : "bg-warning/10 text-warning-text",
                ),
          )}
        >
          {statusText}
        </span>
        <div className="min-w-0 flex-1" />
        <div className="flex shrink-0 items-center gap-1">
          {task?.WorktreePath && localPathActions && (
            <span className="hidden @min-[40rem]/app-header:inline-flex">
              <OpenInEditorButton path={task.WorktreePath} actions={localPathActions} />
            </span>
          )}
          <HeaderIconButton
            label="Keyboard shortcuts"
            testId="app-header-help"
            className="hidden @min-[40rem]/app-header:inline-flex"
            onClick={() => runAction("shortcuts.help", null, { source: "menu" })}
          >
            <Icons.shortcuts aria-hidden />
          </HeaderIconButton>
          {task && onOpenTerminal && (
            <HeaderIconButton label="Open terminal" testId="app-header-terminal" onClick={onOpenTerminal}>
              <Icons.terminal aria-hidden />
            </HeaderIconButton>
          )}
          <AppMenu />
        </div>
        <DesktopWindowControls />
      </header>
    </TooltipProvider>
  );
}
