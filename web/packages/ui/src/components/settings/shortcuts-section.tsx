import { useState } from "react";


import { registerSettingsSection } from "@/components/settings/settings-registry";
import { ShortcutRows } from "@/components/shortcuts-dialog";
import { Input } from "@/components/ui/input";
import { Icons } from "@/lib/icons";

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
      <div className="relative">
        <Icons.search
          aria-hidden
          className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-foreground-muted"
        />
        <Input
          type="search"
          aria-label="Search shortcuts"
          placeholder="Search shortcuts…"
          data-testid="settings-shortcuts-search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-full pl-8"
        />
      </div>
      <ShortcutRows query={query} />
    </div>
  );
}

registerSettingsSection({
  id: "shortcuts",
  label: "Shortcuts",
  order: 120,
  icon: <Icons.shortcuts aria-hidden className="size-3.5" />,
  render: () => <ShortcutsSection />,
});
