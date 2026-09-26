import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";

import "@/components/settings/appearance-section";
import "@/components/settings/connections-section";
import "@/components/settings/daemon-section";
import "@/components/settings/general-section";
import "@/components/settings/notifications-section";
import "@/components/settings/profiles-section";
import "@/components/settings/providers-section";
import "@/components/settings/shortcuts-section";

import { listSettingsSections } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { PaneHeader } from "@/components/ui/pane-header";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";

/**
 * The settings screen (ui-redesign-parity Item 13, reshaped from a Dialog
 * to a full-pane view by web-ui-dogfood-polish Item 4): a section nav
 * rail on the left, the active section's content on the right
 * (audit-paseo.md §4's list+detail shape). The section list itself comes
 * entirely from `settings-registry.ts` -- this component has no knowledge
 * of Appearance/General/Accounts/Quota beyond importing the two built-in
 * section files for their registration side effect, so later sections add
 * themselves the same way without editing this file.
 *
 * Rendered by the shell (App.tsx) as a sibling of the task pane, not by
 * the sidebar: the sidebar's settings button just flips the shell's view
 * state. `onNavigateBack` returns to the previous view (back button, or
 * Escape, which is wired here rather than via a keyboard-registry action
 * because it must not fire while a dialog sits on top of the screen).
 * Paseo's pattern is an expo-router push of `[section].tsx`; here the
 * same root -> section drill-down is one screen with internal state --
 * the plan's "no router library introduction" decision.
 *
 * "Preferences persist client-side (localStorage) unless and until a
 * daemon-side settings API exists" (Item 13's Decisions) is said here,
 * not just implemented silently, so a user does not assume these survive
 * clearing site data or moving to a different browser.
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
  // your place. (Unlike the Dialog this used to be, there is no
  // `open` prop: the screen only mounts while it is the active view.)
  useEffect(() => {
    setActiveId((current) => (current && sections.some((s) => s.id === current) ? current : (sections[0]?.id ?? null)));
    // sections is a fresh array every render (listSettingsSections()), so
    // the dependency below is the ids joined into a string, not the array
    // reference -- otherwise this effect would re-run (and could reset
    // activeId) on every render regardless of whether anything changed.
  }, [sections.map((s) => s.id).join(",")]);

  // Escape closes the whole screen (not just deselects a section) --
  // matching the Dialog Radix used to give this surface for free. Stops
  // propagation so it can't also reach a dialog that happens to be open
  // on top (e.g. a section's own popover).
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      e.stopPropagation();
      onNavigateBack();
    }
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, [onNavigateBack]);

  const active = sections.find((s) => s.id === activeId) ?? null;

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="settings-screen">
      <PaneHeader
        title="Settings"
        testId="settings-screen-header"
        actions={
          <Button
            variant="ghost"
            size="sm"
            aria-label="Back"
            data-testid="settings-back-button"
            onClick={onNavigateBack}
          >
            <ArrowLeft className="size-3.5" />
            Back
          </Button>
        }
      />
      <div className="flex min-h-0 flex-1">
        <nav aria-label="Settings sections" className="w-44 shrink-0 overflow-y-auto border-r p-2">
          <ul className="flex flex-col gap-0.5">
            {sections.map((section) => (
              <li key={section.id}>
                <button
                  type="button"
                  aria-current={section.id === activeId}
                  data-testid={`settings-nav-${section.id}`}
                  onClick={() => setActiveId(section.id)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-ui-base text-foreground hover:bg-hover",
                    section.id === activeId && "bg-selected font-medium",
                  )}
                >
                  {section.icon}
                  {section.label}
                </button>
              </li>
            ))}
          </ul>
        </nav>
        <div className="min-h-0 flex-1 overflow-y-auto p-6">
          {active ? active.render({ client, events }) : <EmptyState testId="settings-empty" title="No settings sections registered" />}
        </div>
      </div>
    </div>
  );
}
