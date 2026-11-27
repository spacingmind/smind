import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Join a plain path, not `new URL(...)` -- that shape is one Vite special-cases
// (asset-URL rewriting) and misresolves under vitest's jsdom environment; see
// no-hardcoded-colors.test.ts.
const SETTINGS_DIR = dirname(fileURLToPath(import.meta.url));
const COMPONENTS_DIR = join(SETTINGS_DIR, "..");

/** A hand-rolled Settings card: `rounded-xl ... border ... bg-card` in one className, in either order. */
const HAND_ROLLED_CARD =
  /rounded-xl[^"'`]*border[^"'`]*bg-card|border[^"'`]*bg-card[^"'`]*rounded-xl/;

/** UI cleanup B2 D6: the source hand-rolls a card or a raw `<h3` instead of rendering through the settings-layout primitives. */
export function matchesHandRolledCardOrRawH3(source: string): boolean {
  return HAND_ROLLED_CARD.test(source) || source.includes("<h3");
}

/**
 * Files that match but are not Settings cards: shared widgets, dialog panels
 * and custom layouts. Every entry must keep matching -- a file that stops
 * matching (converted, moved, deleted) fails the test until its entry is
 * removed, so the list cannot rot.
 */
const ALLOWLIST: Record<string, string> = {
  "settings/add-relay-link.tsx": "relay pairing preview: a two-column dt/dd grid, not a row-divided Settings card",
  "settings/learned-skills-section.tsx":
    "Skills tab frame: fixed min-height so switching tabs never shifts the page; holds its own ul and a nested empty card",
  "settings/role-pack-review.tsx": "role-pack review dialog: seat headings inside a dialog panel",
  "settings/settings-title.tsx":
    "shared SettingsSectionTitle widget: its h3 is the same section heading SettingsGroup renders, for sections that keep their own title row",
  "providers/provider-detail.tsx":
    "Providers detail-pane heading: icon + provider name inside the split-screen shell, not a section title",
  "providers/providers-screen.tsx":
    "Providers master-detail shell: nav and detail panes in one bordered surface, not a Settings card",
};

/** The shared card primitives themselves; they match by definition. */
const EXCLUDED = new Set(["settings/settings-layout.tsx"]);

function scannedFiles(): string[] {
  const files: string[] = [];
  for (const dir of ["settings", "settings/hosts", "providers"]) {
    const full = join(COMPONENTS_DIR, dir);
    if (!existsSync(full)) continue;
    for (const name of readdirSync(full)) {
      const relative = `${dir}/${name}`;
      if (!name.endsWith(".tsx") || name.includes(".test.") || EXCLUDED.has(relative)) continue;
      files.push(relative);
    }
  }
  return files.sort();
}

describe("settings drift", () => {
  it("ui_cleanup_b2_no_hand_rolled_cards_or_raw_h3", () => {
    const scanned = scannedFiles();
    // The scan must stay alive: an empty scan would pass vacuously.
    expect(scanned.length).toBeGreaterThan(0);

    const problems: string[] = [];
    for (const file of scanned) {
      const source = readFileSync(join(COMPONENTS_DIR, file), "utf8");
      const matched = matchesHandRolledCardOrRawH3(source);
      if (matched && !(file in ALLOWLIST)) {
        problems.push(
          `${file}: hand-rolled card or raw <h3> -- render through SettingsGroup/SettingsList/SettingsEmptyCard (or SettingsSectionTitle), or allowlist it with a reason`,
        );
      }
      if (!matched && file in ALLOWLIST) {
        problems.push(`${file}: allowlisted (${ALLOWLIST[file]}) but no longer matches -- remove the stale entry`);
      }
    }
    for (const file of Object.keys(ALLOWLIST)) {
      if (!scanned.includes(file)) {
        problems.push(`${file}: allowlisted (${ALLOWLIST[file]}) but not scanned -- moved or deleted; remove the stale entry`);
      }
    }
    expect(problems).toEqual([]);
  });

  it("drift_matcher_catches_a_hand_rolled_card", () => {
    expect(matchesHandRolledCardOrRawH3('<div className="rounded-xl border bg-card p-3">')).toBe(true);
    expect(matchesHandRolledCardOrRawH3("<h3>")).toBe(true);
    // settings-layout.tsx is excluded by name; the matcher still sees its card pattern.
    expect(matchesHandRolledCardOrRawH3('<div className="rounded-xl border border-border bg-card">')).toBe(true);
  });
});
