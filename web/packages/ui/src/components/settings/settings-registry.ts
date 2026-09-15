import type { ReactNode } from "react";

import type { WsClient } from "@/lib/ws-client";

/** What a registered section's render function receives -- the live daemon connection, so a section (Items 14/15's Accounts/Quota) can make its own RPC calls without SettingsScreen needing to know what those calls are. */
export interface SettingsSectionContext {
  client: WsClient | null;
}

/** One entry in the settings screen's section list (audit-paseo.md §4's list+detail shape). */
export interface SettingsSection {
  /** Stable identity: the list's key, and what a later re-registration under the same id replaces rather than duplicates. */
  id: string;
  label: string;
  icon?: ReactNode;
  /** Lower renders first. Built-in sections use round numbers (100, 200, ...) leaving room for a section to insert itself between two others; ties break by registration order. */
  order: number;
  render: (ctx: SettingsSectionContext) => ReactNode;
}

/**
 * The settings screen's section registry (Item 13: "sections are
 * registered, not hardcoded, so Items 14/15 add their own"). A plain
 * module-level array rather than React context: sections are registered
 * once, at module load, by each section's own file (see
 * appearance-section.tsx/general-section.tsx, which call registerSection
 * at their bottom) -- SettingsScreen only ever reads the list, it never
 * needs to know what's in it.
 *
 * registerSection returns an unregister function, mirroring
 * DaemonEvents.subscribe's shape -- useful for a test that registers a
 * throwaway section and must not leak it into the next test.
 */
const sections: SettingsSection[] = [];

export function registerSettingsSection(section: SettingsSection): () => void {
  sections.push(section);
  return () => {
    const index = sections.indexOf(section);
    if (index !== -1) sections.splice(index, 1);
  };
}

/** Every registered section, sorted by `order` (registration order breaks ties, since Array.prototype.sort is stable). A fresh array each call, so a caller's memo/state never aliases the live registry. */
export function listSettingsSections(): SettingsSection[] {
  return [...sections].sort((a, b) => a.order - b.order);
}
