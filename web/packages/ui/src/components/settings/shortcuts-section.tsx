import { useState } from "react";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { ShortcutRows } from "@/components/shortcuts-dialog";
import { Input } from "@/components/ui/input";

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
      <Input
        type="search"
        aria-label="Search shortcuts"
        placeholder="Search shortcuts…"
        data-testid="settings-shortcuts-search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        className="w-full max-w-sm"
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
