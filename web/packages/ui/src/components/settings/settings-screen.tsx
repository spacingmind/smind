import { Fragment, useEffect, useState } from "react";
import { ArrowLeft, ChevronRight } from "lucide-react";

import "@/components/settings/appearance-section";
import "@/components/settings/connections-section";
import "@/components/settings/daemon-section";
import "@/components/settings/general-section";
import "@/components/settings/mcp-servers-section";
import "@/components/settings/notifications-section";
import "@/components/settings/profiles-section";
import "@/components/settings/providers-section";
import "@/components/settings/shortcuts-section";
import "@/components/settings/usage-section";

import { DesktopWindowControls } from "@/components/desktop-window-controls";
import { listSettingsSections } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useIsMobile } from "@/hooks/use-mobile";
import { useWindowState } from "@/hooks/use-window-state";
import { desktopOS, isDesktop } from "@/lib/platform";
import { cn } from "@/lib/utils";
import { headerLeftInsetPx } from "@/lib/window-chrome";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClient } from "@/lib/ws-client";

/** Tailwind `p-2`, which an inline `paddingLeft` replaces (see AppHeader). */
const BASE_PADDING_PX = 8;

/**
 * The scrolling grouped section nav, shared by both layouts: the desktop
 * sidebar (`variant="sidebar"`, compact h-8 rows with an active highlight)
 * and the narrow list page (`variant="list"`, touch-sized h-11 rows with a
 * trailing chevron and no highlight -- the destination is obvious from the
 * tap).
 */
function SettingsNav({
  sections,
  activeId,
  onSelect,
  variant,
}: {
  sections: ReturnType<typeof listSettingsSections>;
  activeId: string | null;
  onSelect: (id: string) => void;
  variant: "sidebar" | "list";
}) {
  const list = variant === "list";
  return (
    <ul className="flex flex-col gap-0.5">
      {sections.map((section, index) => {
        // A group heading renders once, above its first member --
        // non-clickable, so a group is organization, not a
        // destination of its own.
        const firstOfGroup = section.groupLabel && sections[index - 1]?.groupLabel !== section.groupLabel;
        return (
          <Fragment key={section.id}>
            {firstOfGroup && (
              <li
                aria-hidden="true"
                className={cn("pt-3 pb-1 text-ui-sm font-medium text-foreground-subtle", list ? "px-3" : "px-2")}
              >
                {section.groupLabel}
              </li>
            )}
            <li>
              <button
                type="button"
                aria-current={section.id === activeId}
                data-testid={`settings-nav-${section.id}`}
                onClick={() => onSelect(section.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md text-left text-ui-base text-foreground hover:bg-hover",
                  list ? "h-11 px-3" : "h-8 px-2",
                  !list && section.id === activeId && "bg-selected font-medium",
                )}
              >
                {section.icon}
                {section.label}
                {list && <ChevronRight className="ml-auto size-4 text-foreground-subtle" />}
              </button>
            </li>
          </Fragment>
        );
      })}
    </ul>
  );
}

/**
 * The settings screen. Desktop (>=768px) is a full-viewport two-column
 * layout (Paseo's settings screen and ZCode's SettingsPage are the
 * references): a left sidebar with a back-to-app row above a scrolling
 * grouped section nav, and a framed content panel on the right with an
 * h-12 header (group / section title) and a scrolling body. Narrow
 * (<768px) is a list-to-detail drill-down: the list page shows the grouped
 * nav as touch-sized rows, and tapping one pushes that section's detail
 * page with a back-arrow header.
 *
 * In the desktop build the sidebar's top strip and the content header are
 * window title-bar drag regions: the headers pad clear of the macOS
 * traffic lights, and Windows/Linux end the content header in the drawn
 * caption buttons (see AppHeader / DesktopWindowControls).
 *
 * The section list itself comes entirely from `settings-registry.ts` --
 * this component has no knowledge of individual sections beyond importing
 * their files for the registration side effect.
 *
 * Mounted by App.tsx as an absolutely positioned full-screen overlay over
 * the (hidden, inert) shell. `onNavigateBack` returns to the previous view
 * (back button, or Escape -- which only fires when nothing else consumed
 * it: not default-prevented, no live dialog/menu on top; on a narrow
 * detail page Escape first pops back to the list).
 *
 * "Preferences persist client-side (localStorage) unless and until a
 * daemon-side settings API exists" is said here, not just implemented
 * silently, so a user does not assume these survive clearing site data
 * or moving to a different browser.
 */
export function SettingsScreen({
  client,
  events = null,
  onNavigateBack,
  initialSectionId,
}: {
  client: WsClient | null;
  /** The app's shared events.subscribe surface, forwarded to each section's render context -- optional (defaults null) so existing callers/tests that don't care about live updates need no change. */
  events?: DaemonEvents | null;
  /** Returns to the view the settings screen was opened from (back button / Escape). */
  onNavigateBack: () => void;
  /** Which section a fresh mount opens to, if it exists in the registry -- `shortcuts.help` (AC5) deep-links to "shortcuts" this way. Defaults to the first registered section, same as before this prop existed. */
  initialSectionId?: string;
}) {
  const sections = listSettingsSections();
  const [activeId, setActiveId] = useState<string | null>(
    () => sections.find((s) => s.id === initialSectionId)?.id ?? sections[0]?.id ?? null,
  );
  const narrow = useIsMobile();
  // Initial "detail" (not derived from `narrow`, which is false on the
  // very first render -- useIsMobile only settles inside an effect), so a
  // deep link lands on its section's detail once the narrow layout kicks
  // in, while a plain open starts on the list.
  const [view, setView] = useState<"list" | "detail">(initialSectionId ? "detail" : "list");

  // Re-derive the active section whenever the registry changes shape, but
  // only snap to the first section when the current selection no longer
  // exists -- so re-opening on the same section you left doesn't reset
  // your place.
  useEffect(() => {
    setActiveId((current) => (current && sections.some((s) => s.id === current) ? current : (sections[0]?.id ?? null)));
    // sections is a fresh array every render (listSettingsSections()), so
    // the dependency below is the ids joined into a string, not the array
    // reference -- otherwise this effect would re-run (and could reset
    // activeId) on every render regardless of whether anything changed.
  }, [sections.map((s) => s.id).join(",")]);

  // Escape pops a narrow detail page back to the list, and otherwise
  // closes the whole screen -- but only when nothing else consumed it:
  // default-prevented events and any live dialog/menu are left alone
  // (bubble phase, so inner handlers get first crack). A dialog inside an
  // inert subtree (e.g. the hidden shell behind this screen) is not live
  // and does not count.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      if (e.defaultPrevented) return;
      const openDialogs = document.querySelectorAll('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]');
      if (Array.from(openDialogs).some((el) => !el.closest("[inert]"))) return;
      if (narrow && view === "detail") setView("list");
      else onNavigateBack();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [narrow, view, onNavigateBack]);

  const active = sections.find((s) => s.id === activeId) ?? null;
  const dragRegion = isDesktop ? "deep" : undefined;
  const { fullscreen } = useWindowState();
  const leftInset = isDesktop ? headerLeftInsetPx(desktopOS, fullscreen, 0) : 0;
  const headerStyle = leftInset > 0 ? { paddingLeft: BASE_PADDING_PX + leftInset } : undefined;

  if (narrow && view === "list") {
    return (
      <div className="flex h-full min-h-0 w-full flex-col bg-background" data-testid="settings-screen">
        <header
          className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
          data-testid="settings-screen-header"
          data-tauri-drag-region={dragRegion}
          style={headerStyle}
        >
          <Button
            variant="ghost"
            size="icon"
            aria-label="Back to app"
            data-testid="settings-back-button"
            onClick={onNavigateBack}
          >
            <ArrowLeft className="size-4" />
          </Button>
          <h1 className="truncate text-ui-base font-medium">Settings</h1>
          <div className="ml-auto" />
          <DesktopWindowControls />
        </header>
        <nav aria-label="Settings sections" className="min-h-0 flex-1 overflow-y-auto p-2">
          <SettingsNav
            sections={sections}
            activeId={activeId}
            onSelect={(id) => {
              setActiveId(id);
              setView("detail");
            }}
            variant="list"
          />
        </nav>
      </div>
    );
  }

  if (narrow && view === "detail") {
    return (
      <div className="flex h-full min-h-0 w-full flex-col bg-background" data-testid="settings-screen">
        <header
          className="flex h-12 shrink-0 items-center gap-2 border-b p-2"
          data-testid="settings-screen-header"
          data-tauri-drag-region={dragRegion}
          style={headerStyle}
        >
          <Button
            variant="ghost"
            size="icon"
            aria-label="Back to settings"
            data-testid="settings-detail-back"
            onClick={() => setView("list")}
          >
            <ArrowLeft className="size-4" />
          </Button>
          <h1 className="truncate text-ui-base font-medium">{active?.label}</h1>
          <div className="ml-auto" />
          <DesktopWindowControls />
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto" data-testid="settings-content">
          <div className="w-full p-4">
            {active ? active.render({ client, events }) : <EmptyState testId="settings-empty" title="No settings sections registered" />}
          </div>
        </main>
      </div>
    );
  }

  const backToApp = (
    <Button
      variant="ghost"
      aria-label="Back to app"
      className="h-8 w-full justify-start gap-2 rounded-md px-2 text-ui-base"
      data-testid="settings-back-button"
      onClick={onNavigateBack}
    >
      <ArrowLeft className="size-3.5" />
      Back to app
    </Button>
  );

  return (
    <div className="flex h-full min-h-0 w-full" data-testid="settings-screen">
      <aside
        className="flex w-64 shrink-0 flex-col border-r bg-sidebar text-sidebar-foreground"
        data-settings-sidebar
        data-testid="settings-sidebar"
      >
        {/* Desktop build: an empty h-12 strip owns the title-bar drag and the
            macOS traffic lights, the back row sits under it. Browser: no title
            bar to clear, so the back row takes that h-12 row itself and lines
            up with the content header across the divider. */}
        {isDesktop ? (
          <>
            <div className="h-12 shrink-0" data-testid="settings-sidebar-top" data-tauri-drag-region={dragRegion} />
            <div className="shrink-0 px-2 pb-1">{backToApp}</div>
          </>
        ) : (
          <div className="flex h-12 shrink-0 items-center px-2" data-testid="settings-sidebar-top">
            {backToApp}
          </div>
        )}
        <nav aria-label="Settings sections" className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
          <SettingsNav
            sections={sections}
            activeId={activeId}
            onSelect={setActiveId}
            variant="sidebar"
          />
        </nav>
      </aside>
      <section className="flex min-h-0 min-w-0 flex-1 flex-col bg-background p-1 pl-0">
        <div
          className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-background"
          data-testid="settings-content-frame"
        >
          <header
            className="flex h-12 shrink-0 items-center gap-2 border-b p-2 pl-6"
            data-testid="settings-screen-header"
            data-tauri-drag-region={dragRegion}
          >
            {active?.groupLabel && (
              <>
                <span className="text-ui-base text-foreground-subtle">{active.groupLabel}</span>
                <span aria-hidden className="text-foreground-subtle">
                  /
                </span>
              </>
            )}
            <h1 className="truncate text-ui-base font-medium">{active?.label}</h1>
            <div className="ml-auto" />
            <DesktopWindowControls />
          </header>
          <main className="min-h-0 flex-1 overflow-y-auto" data-testid="settings-content">
            <div className="mx-auto w-full max-w-3xl p-6">
              {active ? (
                active.render({ client, events })
              ) : (
                <EmptyState testId="settings-empty" title="No settings sections registered" />
              )}
            </div>
          </main>
        </div>
      </section>
    </div>
  );
}
