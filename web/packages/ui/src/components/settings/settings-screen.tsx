import { useEffect, useState } from "react";

import "@/components/settings/appearance-section";
import "@/components/settings/general-section";

import { listSettingsSections } from "@/components/settings/settings-registry";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState } from "@/components/ui/empty-state";
import { cn } from "@/lib/utils";
import type { WsClient } from "@/lib/ws-client";

/**
 * The settings shell (ui-redesign-parity plan, Item 13): a section list
 * down the left, the active section's content on the right
 * (audit-paseo.md §4's list+detail shape). The section list itself comes
 * entirely from `settings-registry.ts` -- this component has no
 * knowledge of Appearance/General/Accounts/Quota beyond importing the two
 * built-in section files for their registration side effect, so Items
 * 14/15 add themselves the same way without editing this file.
 *
 * Rendered from a dialog for now (Item 13's own entry point, from
 * app-sidebar.tsx's settings button) rather than a route: this track owns
 * app-sidebar.tsx and components/settings/*, not App.tsx's routing, so
 * wiring a `/settings` URL and a keyboard shortcut to the same
 * `useSettingsOpen` state this dialog already takes is left to Track A
 * (see the plan's Item 13 note and its Tracks section on `App.tsx`
 * coordination).
 *
 * "Preferences persist client-side (localStorage) unless and until a
 * daemon-side settings API exists" (Item 13's Decisions) is said here,
 * not just implemented silently, so a user does not assume these survive
 * clearing site data or moving to a different browser.
 */
export function SettingsScreen({
  client,
  open,
  onOpenChange,
}: {
  client: WsClient | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const sections = listSettingsSections();
  const [activeId, setActiveId] = useState<string | null>(sections[0]?.id ?? null);

  // Re-derive the active section whenever the dialog opens (a section
  // could have been registered/unregistered while it was closed) or the
  // registry itself changes shape, but only snap to the first section
  // when the current selection no longer exists -- so re-opening on the
  // same section you left doesn't reset your place.
  useEffect(() => {
    if (!open) return;
    setActiveId((current) => (current && sections.some((s) => s.id === current) ? current : (sections[0]?.id ?? null)));
    // sections is a fresh array every render (listSettingsSections()), so
    // the dependency below is the ids joined into a string, not the array
    // reference -- otherwise this effect would re-run (and could reset
    // activeId) on every render regardless of whether anything changed.
  }, [open, sections.map((s) => s.id).join(",")]);

  const active = sections.find((s) => s.id === activeId) ?? null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[32rem] max-w-3xl flex-col gap-0 p-0 sm:max-w-3xl" data-testid="settings-screen">
        <DialogHeader className="border-b px-6 py-4">
          <DialogTitle>Settings</DialogTitle>
        </DialogHeader>
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
                      "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-foreground hover:bg-accent",
                      section.id === activeId && "bg-accent font-medium",
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
            {active ? active.render({ client }) : <EmptyState testId="settings-empty" title="No settings sections registered" />}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
