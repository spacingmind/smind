import { Fragment, useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";

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

import { listSettingsSections } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";

/**
 * The settings screen, as a full-viewport two-column layout (Paseo's
 * settings screen and ZCode's SettingsPage are the references): a left
 * sidebar with a back-to-app row above a scrolling grouped section nav,
 * and a framed content panel on the right with an h-12 header (group /
 * section title) and a scrolling body. The section list itself comes
 * entirely from `settings-registry.ts` -- this component has no knowledge
 * of individual sections beyond importing their files for the
 * registration side effect.
 *
 * Mounted by App.tsx as an absolutely positioned full-screen overlay over
 * the (hidden, inert) shell. `onNavigateBack` returns to the previous
 * view (back button, or Escape -- which only fires when nothing else
 * consumed it: not default-prevented, no dialog/menu on top).
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

  // Escape closes the whole screen, but only when nothing else consumed
  // it: default-prevented events and any open dialog/menu are left alone
  // (bubble phase, so inner handlers get first crack).
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      if (e.defaultPrevented) return;
      if (document.querySelector('[role="dialog"],[role="alertdialog"],[role="menu"],[role="listbox"]')) return;
      onNavigateBack();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onNavigateBack]);

  const active = sections.find((s) => s.id === activeId) ?? null;

  return (
    <div className="flex h-full min-h-0 w-full bg-background" data-testid="settings-screen">
      <aside
        className="flex w-64 shrink-0 flex-col border-r bg-sidebar text-sidebar-foreground"
        data-settings-sidebar
        data-testid="settings-sidebar"
      >
        <div className="h-12 shrink-0" data-testid="settings-sidebar-top" />
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
        <nav aria-label="Settings sections" className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
          <ul className="flex flex-col gap-0.5">
            {sections.map((section, index) => {
              // A group heading renders once, above its first member --
              // non-clickable, so a group is organization, not a
              // destination of its own.
              const firstOfGroup = section.groupLabel && sections[index - 1]?.groupLabel !== section.groupLabel;
              return (
                <Fragment key={section.id}>
                  {firstOfGroup && (
                    <li aria-hidden="true" className="px-2 pt-3 pb-1 text-ui-sm font-medium text-foreground-subtle">
                      {section.groupLabel}
                    </li>
                  )}
                  <li>
                    <button
                      type="button"
                      aria-current={section.id === activeId}
                      data-testid={`settings-nav-${section.id}`}
                      onClick={() => setActiveId(section.id)}
                      className={cn(
                        "flex h-8 w-full items-center gap-2 rounded-md px-2 pl-2 text-left text-ui-base text-foreground hover:bg-hover",
                        section.id === activeId && "bg-selected font-medium",
                      )}
                    >
                      {section.icon}
                      {section.label}
                    </button>
                  </li>
                </Fragment>
              );
            })}
          </ul>
        </nav>
      </aside>
      <section className="flex min-h-0 min-w-0 flex-1 flex-col p-1 pl-0">
        <div
          className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-background"
          data-testid="settings-content-frame"
        >
          <header className="flex h-12 shrink-0 items-center gap-2 border-b px-6" data-testid="settings-screen-header">
            {active?.groupLabel && (
              <>
                <span className="text-ui-base text-foreground-subtle">{active.groupLabel}</span>
                <span aria-hidden className="text-foreground-subtle">
                  /
                </span>
              </>
            )}
            <h1 className="truncate text-ui-base font-medium">{active?.label}</h1>
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
