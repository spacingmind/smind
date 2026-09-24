import { useState } from "react";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { ShortcutRows } from "@/components/shortcuts-dialog";

/**
 * AC5 of `docs/plans/active/web-keyboard-tabs.md`: rebinding's permanent
 * home, replacing the old standalone `Shift+?` dialog (`shortcuts.help`
 * now deep-links here instead -- App.tsx). `<ShortcutRows />` already
 * carries search/rebind/reset/reset-all/conflicts; this file is just the
 * section registration plus the search box, matching the shape of
 * `appearance-section.tsx`/`general-section.tsx`.
 */
export function ShortcutsSection() {
  const [query, setQuery] = useState("");

  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-shortcuts">
      <input
        type="search"
        aria-label="Search shortcuts"
        placeholder="Search shortcuts…"
        data-testid="settings-shortcuts-search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        className="h-8 w-full max-w-sm rounded-lg border border-input bg-transparent px-2 text-ui-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
      />
      <ShortcutRows query={query} />
    </div>
  );
}

registerSettingsSection({
  id: "shortcuts",
  label: "Shortcuts",
  order: 300,
  render: () => <ShortcutsSection />,
});
