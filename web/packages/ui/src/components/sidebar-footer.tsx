import { useState, type ComponentType, type ReactNode } from "react";
import { Bot, CircleGauge, CircleHelp, FolderPlus, KeyRound, Server, Settings as SettingsIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import { ThemeToggle } from "@/components/theme-toggle";
import { openUsageModal } from "@/hooks/use-usage-modal";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import type { ConnectionStatus } from "@/lib/reconnect";
import { desktop, isDesktop } from "@/lib/platform";
import { cn } from "@/lib/utils";

const DOCS_URL = "https://github.com/spacingmind/smind#readme";
const ISSUES_URL = "https://github.com/spacingmind/smind/issues/new";

const ICON_BUTTON_CLASS = "text-foreground-muted hover:text-foreground";

function openExternal(url: string): void {
  if (isDesktop) {
    desktop.openExternal(url).catch(() => {});
    return;
  }
  window.open(url, "_blank", "noopener,noreferrer");
}

export function providerTone(health: { healthy: number; total: number } | null): StatusDotStatus | null {
  if (!health || health.total === 0) return null;
  if (health.healthy === health.total) return "success";
  return health.healthy > 0 ? "warning" : "danger";
}

function FooterTooltip({
  label,
  children,
  tooltip,
}: {
  label: string;
  children: ReactNode;
  /** From useMenuTooltip, for a trigger that also opens a menu/popover. */
  tooltip?: ReturnType<typeof useMenuTooltip>["tooltip"];
}) {
  return (
    <Tooltip {...tooltip}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent side="top">{label}</TooltipContent>
    </Tooltip>
  );
}

function FooterIconButton({
  label,
  icon: Icon,
  testId,
  onClick,
}: {
  label: string;
  icon: ComponentType;
  testId: string;
  onClick: () => void;
}) {
  return (
    <FooterTooltip label={label}>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={label}
        data-testid={testId}
        className={ICON_BUTTON_CLASS}
        onClick={onClick}
      >
        <Icon />
      </Button>
    </FooterTooltip>
  );
}

function HostRow({
  icon: Icon,
  label,
  value,
  testId,
  onSelect,
}: {
  icon?: ComponentType<{ className?: string }>;
  label: string;
  value?: ReactNode;
  testId: string;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      data-testid={testId}
      onClick={onSelect}
      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-ui-sm text-foreground-subtle outline-none hover:bg-accent hover:text-accent-foreground focus-visible:bg-accent"
    >
      {Icon && <Icon className="size-4 shrink-0" />}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {value != null && <span className="flex items-center gap-1.5 tabular-nums text-foreground-subtlest">{value}</span>}
    </button>
  );
}

function HostButton({
  connectionStatus,
  connectionStatusText,
  providerHealth,
  agentCount,
  onOpenSettings,
}: {
  connectionStatus: ConnectionStatus;
  connectionStatusText?: string;
  providerHealth: { healthy: number; total: number } | null;
  agentCount: number;
  onOpenSettings?: (sectionId?: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const hostTip = useMenuTooltip();
  const tone = providerTone(providerHealth);
  const dot: StatusDotStatus | null = connectionStatus !== "connected" ? "danger" : tone;

  function select(sectionId: string) {
    setOpen(false);
    onOpenSettings?.(sectionId);
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        hostTip.onMenuOpenChange(next);
      }}
    >
      <FooterTooltip label="Host" tooltip={hostTip.tooltip}>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Host"
            data-testid="sidebar-footer-host"
            className={cn("relative", ICON_BUTTON_CLASS)}
          >
            <Server />
            {dot && (
              <StatusDot
                status={dot}
                data-testid="sidebar-footer-host-dot"
                className="absolute right-1 top-1 ring-2 ring-sidebar"
              />
            )}
          </Button>
        </PopoverTrigger>
      </FooterTooltip>
      <PopoverContent side="top" align="start" className="w-64 p-1" data-testid="sidebar-footer-host-popover">
        <div
          data-testid="sidebar-footer-host-connection"
          className="flex items-center gap-2 px-2 py-1.5 text-ui-sm text-foreground-subtle"
        >
          <StatusDot status={connectionStatus === "connected" ? "success" : "danger"} />
          <span className="min-w-0 flex-1 truncate">Connection</span>
          <span className="text-foreground-subtlest">{connectionStatusText ?? connectionStatus}</span>
        </div>
        <HostRow
          icon={KeyRound}
          label="Providers"
          testId="sidebar-footer-host-providers"
          onSelect={() => select("providers")}
          value={
            providerHealth &&
            providerHealth.total > 0 && (
              <>
                <StatusDot status={tone ?? "neutral"} data-testid="sidebar-footer-providers-health-dot" />
                {providerHealth.healthy} healthy
              </>
            )
          }
        />
        <HostRow
          icon={Bot}
          label="Agents"
          testId="sidebar-footer-host-agents"
          onSelect={() => select("agents")}
          value={agentCount}
        />
        {isDesktop && (
          <>
            <HostRow label="Daemon" testId="sidebar-footer-host-daemon" onSelect={() => select("daemon")} />
            <HostRow
              label="Daemon server"
              testId="sidebar-footer-host-connections"
              onSelect={() => select("connections")}
            />
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}

function HelpButton({ onOpenSettings }: { onOpenSettings?: (sectionId?: string) => void }) {
  const { runAction } = useKeyboard();
  const helpTip = useMenuTooltip();
  return (
    <DropdownMenu onOpenChange={helpTip.onMenuOpenChange}>
      <FooterTooltip label="Help & support" tooltip={helpTip.tooltip}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Help & support"
            data-testid="sidebar-footer-help"
            className={ICON_BUTTON_CLASS}
          >
            <CircleHelp />
          </Button>
        </DropdownMenuTrigger>
      </FooterTooltip>
      <DropdownMenuContent side="top" align="end">
        <DropdownMenuItem data-testid="sidebar-footer-help-shortcuts" onSelect={() => onOpenSettings?.("shortcuts")}>
          Keyboard shortcuts
        </DropdownMenuItem>
        <DropdownMenuItem data-testid="sidebar-footer-help-palette" onSelect={() => runAction("palette.open")}>
          Command palette
        </DropdownMenuItem>
        <DropdownMenuItem data-testid="sidebar-footer-help-docs" onSelect={() => openExternal(DOCS_URL)}>
          Documentation
        </DropdownMenuItem>
        <DropdownMenuItem data-testid="sidebar-footer-help-issue" onSelect={() => openExternal(ISSUES_URL)}>
          Report an issue
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * The sidebar's bottom region (Paseo `SidebarFooter` shape): an optional
 * rows slot above one icon line. Expanded it is a single row; on the
 * icon-collapsed rail the same buttons stack vertically (CSS only, via
 * `group-data-[collapsible=icon]`).
 *
 * `rows` is the slot for the plan-usage summary (ADR-0028 PR-D2, off by
 * default): AppSidebar passes it only while "Show summary in sidebar" is on,
 * and the wrapper hides itself while its content renders nothing.
 */
export function SidebarFooterActions({
  rows,
  connectionStatus = "connected",
  connectionStatusText,
  providerHealth,
  agentCount,
  onAddWorkspace,
  onOpenSettings,
}: {
  rows?: ReactNode;
  connectionStatus?: ConnectionStatus;
  connectionStatusText?: string;
  providerHealth: { healthy: number; total: number } | null;
  agentCount: number;
  onAddWorkspace: () => void;
  onOpenSettings?: (sectionId?: string) => void;
}) {
  return (
    <TooltipProvider>
      <div data-testid="sidebar-footer" className="flex flex-col border-t">
        {rows ? (
          <div data-testid="sidebar-footer-rows" className="flex flex-col gap-1 px-2 pt-2 empty:hidden group-data-[collapsible=icon]:hidden">
            {rows}
          </div>
        ) : null}
        <div
          data-testid="sidebar-footer-actions"
          className="flex items-center p-2 group-data-[collapsible=icon]:flex-col group-data-[collapsible=icon]:gap-1 group-data-[collapsible=icon]:px-0"
        >
          <FooterIconButton
            label="Add workspace"
            icon={FolderPlus}
            testId="sidebar-footer-add-workspace"
            onClick={onAddWorkspace}
          />
          <FooterIconButton
            label="Usage"
            icon={CircleGauge}
            testId="sidebar-footer-usage"
            onClick={openUsageModal}
          />
          <HostButton
            connectionStatus={connectionStatus}
            connectionStatusText={connectionStatusText}
            providerHealth={providerHealth}
            agentCount={agentCount}
            onOpenSettings={onOpenSettings}
          />
          <div data-testid="sidebar-footer-spacer" className="flex-1 group-data-[collapsible=icon]:hidden" />
          <HelpButton onOpenSettings={onOpenSettings} />
          <ThemeToggle withTooltip className={ICON_BUTTON_CLASS} />
          <FooterIconButton
            label="Settings"
            icon={SettingsIcon}
            testId="sidebar-settings-button"
            onClick={() => onOpenSettings?.()}
          />
        </div>
      </div>
    </TooltipProvider>
  );
}
