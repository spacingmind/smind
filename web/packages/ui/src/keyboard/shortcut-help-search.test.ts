import { describe, expect, it } from "vitest";

import { filterShortcutHelpSections } from "@/keyboard/shortcut-help-search";
import { helpSections, resolveBindings } from "@/keyboard/shortcuts";

describe("filterShortcutHelpSections", () => {
  const sections = helpSections(resolveBindings(), false);

  it("returns every section unchanged for an empty query", () => {
    expect(filterShortcutHelpSections(sections, "")).toEqual(sections);
    expect(filterShortcutHelpSections(sections, "   ")).toEqual(sections);
  });

  it("matches a row by its label, case-insensitively", () => {
    const filtered = filterShortcutHelpSections(sections, "command palette");
    const rowIds = filtered.flatMap((s) => s.rows.map((r) => r.id));
    expect(rowIds).toContain("palette-open");
  });

  it("matches a row by its formatted keys", () => {
    const filtered = filterShortcutHelpSections(sections, "ctrl+k");
    const rowIds = filtered.flatMap((s) => s.rows.map((r) => r.id));
    expect(rowIds).toContain("palette-open");
  });

  it("matches 'cmd' and 'command' against a Mod-bound row even off mac", () => {
    for (const query of ["cmd", "command", "ctrl", "control"]) {
      const rowIds = filterShortcutHelpSections(sections, query).flatMap((s) => s.rows.map((r) => r.id));
      expect(rowIds).toContain("palette-open");
    }
  });

  it("keeps every row of a section whose own title matches", () => {
    const filtered = filterShortcutHelpSections(sections, "tabs");
    const tabsSection = filtered.find((s) => s.id === "tabs")!;
    const fullTabsSection = sections.find((s) => s.id === "tabs")!;
    expect(tabsSection.rows).toEqual(fullTabsSection.rows);
  });

  it("drops a section with no matching rows entirely", () => {
    const filtered = filterShortcutHelpSections(sections, "nothing matches this query");
    expect(filtered).toEqual([]);
  });

  it("matches an unassigned row's label even though it has no keys to search", () => {
    const unassigned = resolveBindings(undefined, { "palette-open": "" });
    const unassignedSections = helpSections(unassigned, false);
    const filtered = filterShortcutHelpSections(unassignedSections, "command palette");
    expect(filtered.flatMap((s) => s.rows.map((r) => r.id))).toContain("palette-open");
  });
});
