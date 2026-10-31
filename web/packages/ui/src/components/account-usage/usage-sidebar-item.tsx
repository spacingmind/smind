import { Fragment, useCallback, useMemo, useRef, useState } from "react";
import { Icons } from "@/lib/icons";

import { UsageMeter } from "@/components/account-usage/usage-meter";
import { UsageSourceIcon } from "@/components/account-usage/usage-source-icon";
import { useSidebar } from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useUsageReports } from "@/hooks/use-usage-reports";
import { openUsageModal } from "@/hooks/use-usage-modal";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import { resolveAccountFilter } from "@/lib/account-usage/model";
import {
  choosePinnedUsageLayout,
  METER_GAP,
  MIN_METER_WIDTH,
  resolvePinnedUsage,
  type PinnedUsageLayout,
  type PinnedUsageSource,
  type PinnedUsageWindow,
} from "@/lib/account-usage/pinned";
import type { UsageTone } from "@/lib/account-usage/types";
import type { WsClient } from "@/lib/ws-client";
import { cn } from "@/lib/utils";

const TEXT_TONE: Record<UsageTone, string> = {
  ok: "text-foreground",
  default: "text-foreground",
  warning: "text-warning-text",
  danger: "text-destructive",
};

/** A meter stays neutral until a window needs attention, so a footer of healthy windows is calm. */
function isQuiet(tone: UsageTone): boolean {
  return tone === "ok" || tone === "default";
}

/** The content width of an element, kept current by a ResizeObserver; null until it has been measured. */
function useObservedWidth(): [(element: HTMLElement | null) => void, number | null] {
  const [width, setWidth] = useState<number | null>(null);
  const observer = useRef<ResizeObserver | null>(null);
  const ref = useCallback((element: HTMLElement | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!element || typeof ResizeObserver === "undefined") return;
    const next = new ResizeObserver((entries) => {
      const entry = entries[entries.length - 1];
      if (entry) setWidth(entry.contentRect.width);
    });
    next.observe(element);
    observer.current = next;
  }, []);
  return [ref, width];
}

function describeSources(sources: readonly PinnedUsageSource[]): string[] {
  return sources.flatMap((source) => source.windows.map((window) => window.label));
}

/**
 * One line: each account's logo, then per window a meter and "31% 5h". Off-screen copies measure
 * the line with labels and with percents alone; the richest layout that fits is shown, and what
 * it leaves out takes no space at all (Paseo's PinnedUsageTrigger, with ResizeObserver for
 * onLayout). The line never wraps or overflows.
 */
function PinnedUsageLine({
  sources,
  layout,
  measuring = false,
}: {
  sources: readonly PinnedUsageSource[];
  layout: PinnedUsageLayout;
  measuring?: boolean;
}) {
  return (
    <>
      {sources.map((source, sourceIndex) => (
        <Fragment key={source.key}>
          <span
            className={cn("flex shrink-0 items-center", sourceIndex > 0 && "ml-3")}
            data-testid={measuring ? undefined : "sidebar-usage-source"}
          >
            <UsageSourceIcon provider={source.provider} className="size-3.5" />
          </span>
          {source.windows.map((window, windowIndex) => (
            <PinnedWindow
              key={window.key}
              window={window}
              layout={layout}
              leadClass={windowIndex === 0 ? "ml-1.5" : "ml-2"}
              measuring={measuring}
            />
          ))}
        </Fragment>
      ))}
    </>
  );
}

function PinnedWindow({
  window,
  layout,
  leadClass,
  measuring,
}: {
  window: PinnedUsageWindow;
  layout: PinnedUsageLayout;
  leadClass: string;
  measuring: boolean;
}) {
  return (
    <>
      {layout === "meters" ? (
        <span
          className={cn("flex-1 basis-0", leadClass)}
          style={{ minWidth: MIN_METER_WIDTH, marginRight: METER_GAP }}
          data-testid={measuring ? undefined : "sidebar-usage-meter"}
        >
          <UsageMeter percent={window.percent} tone={window.tone} quiet={isQuiet(window.tone)} className="h-1.5" />
        </span>
      ) : null}
      <span
        className={cn("shrink-0 whitespace-nowrap text-ui-sm tabular-nums", TEXT_TONE[window.tone], layout !== "meters" && leadClass)}
        data-tone={window.tone}
        data-testid={measuring ? undefined : "sidebar-usage-pinned-window"}
      >
        {window.percentText}
        {layout === "percents" || window.shortLabel === "" ? null : (
          <span className="text-foreground-muted">{` ${window.shortLabel}`}</span>
        )}
      </span>
    </>
  );
}

const ROW_CLASS =
  "relative flex h-7 w-full min-w-0 items-center overflow-hidden rounded-md px-2 text-left outline-none transition-colors hover:bg-hover focus-visible:ring-2 focus-visible:ring-sidebar-ring";

function PinnedUsageTrigger({ sources }: { sources: readonly PinnedUsageSource[] }) {
  const [availableRef, available] = useObservedWidth();
  const [labelsRef, labelsWidth] = useObservedWidth();
  const [percentsRef, percentsWidth] = useObservedWidth();
  const windowCount = sources.reduce((count, source) => count + source.windows.length, 0);
  const layout = choosePinnedUsageLayout({ available, labelsWidth, percentsWidth, windowCount });
  const lines = describeSources(sources);
  // Even the smallest layout can be wider than the row (several accounts at the minimum sidebar
  // width): the line is clipped, and a fade makes the cut read as intentional. The tooltip and
  // accessible name still carry every window.
  const shownWidth = layout === "percents" ? percentsWidth : layout === "labels" ? labelsWidth : null;
  const clipped = available !== null && shownWidth !== null && shownWidth > available;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={openUsageModal}
          aria-label={`Usage: ${lines.join(", ")}`}
          data-testid="sidebar-usage-summary"
          data-layout={layout}
          data-clipped={clipped || undefined}
          className={ROW_CLASS}
        >
          <span
            ref={availableRef}
            className={cn(
              "flex min-w-0 flex-1 items-center overflow-hidden",
              clipped && "[mask-image:linear-gradient(to_right,#000_calc(100%_-_28px),transparent)]",
            )}
          >
            <PinnedUsageLine sources={sources} layout={layout} />
          </span>
          <span aria-hidden className="pointer-events-none invisible absolute top-0 left-0 w-[10000px]">
            <span ref={labelsRef} className="flex w-fit items-center">
              <PinnedUsageLine sources={sources} layout="labels" measuring />
            </span>
            <span ref={percentsRef} className="flex w-fit items-center">
              <PinnedUsageLine sources={sources} layout="percents" measuring />
            </span>
          </span>
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" className="flex flex-col gap-0.5">
        {lines.map((line, index) => (
          <span key={index}>{line}</span>
        ))}
      </TooltipContent>
    </Tooltip>
  );
}

/** The row with no data to summarise: the gauge icon and "Usage", still one click from the modal. */
function PlainUsageTrigger() {
  return (
    <button type="button" onClick={openUsageModal} aria-label="Usage" data-testid="sidebar-usage-summary" data-layout="plain" className={cn(ROW_CLASS, "gap-2")}>
      <Icons.usage aria-hidden className="size-4 shrink-0 text-foreground-muted" />
      <span className="min-w-0 truncate text-ui-sm text-foreground-muted">Usage</span>
    </button>
  );
}

/** Mounted only while the row is on and visible: it is the touching surface (ADR-0028 AC54). */
function UsageSidebarSummary() {
  const { snapshot } = useUsageReports({ touch: true });
  const { preferences } = useUsagePreferences();
  const filterKey = resolveAccountFilter(preferences.accountKey, snapshot.reports);
  const sources = useMemo(
    () => resolvePinnedUsage(filterKey ? snapshot.reports.filter((usage) => usage.key === filterKey) : snapshot.reports, preferences),
    [filterKey, preferences, snapshot.reports],
  );
  // The row exists only while polling is enabled and the daemon has the RPC.
  if (!snapshot.supportsUsage || !snapshot.enabled) return null;
  return sources.length > 0 ? <PinnedUsageTrigger sources={sources} /> : <PlainUsageTrigger />;
}

/**
 * The sidebar footer's plan-usage summary (ADR-0028 PR-D2): each pinned or default window's
 * provider logo and percent, one line, tone coloured; pressing it opens the Usage modal. The
 * caller renders it only while "Show summary in sidebar" is on. On the icon-collapsed rail it
 * renders nothing -- the rail already has the footer Usage icon, a line of percents cannot
 * fit 48px, and an unseen row must not keep touching the daemon. The connection comes from
 * `useDaemonConnection`; the props are accepted only until the sidebar stops passing them.
 */
export function UsageSidebarItem(_props: { client?: WsClient | null; events?: DaemonEvents | null } = {}) {
  const { state, isMobile } = useSidebar();
  if (state === "collapsed" && !isMobile) return null;
  return <UsageSidebarSummary />;
}
