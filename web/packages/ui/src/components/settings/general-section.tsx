import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";

/**
 * Item 13's General section originally held the composer's two defaults
 * (default provider / default approval policy). The run-config IA plan
 * replaced both with the ★ default agent in Settings -> Agents (it seeds
 * all three composer fields at once, not just two, and ties them to a
 * named, reusable agent rather than two independent preferences) -- see
 * that plan's Decisions section for why keeping both would have been two
 * sources of truth for "what does a new task start with".
 *
 * General stays registered (still first in the regrouped nav: General ·
 * Appearance · Agents & providers · Connection · Notifications ·
 * Shortcuts) with a pointer to where that setting moved, rather than
 * disappearing outright or silently landing on the next section.
 */
export function GeneralSection(_ctx: SettingsSectionContext) {
  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-general">
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Defaults for new tasks</h3>
        <p className="text-ui-base text-muted-foreground">
          Set a ★ default agent in{" "}
          <span className="font-medium text-foreground">Settings → Agents</span> — a new task's composer starts
          from it.
        </p>
      </section>
    </div>
  );
}

registerSettingsSection({
  id: "general",
  label: "General",
  // First in the nav (run-config IA regroup: General · Appearance ·
  // Agents & providers · Connection · Notifications · Shortcuts).
  order: 50,
  render: (ctx) => <GeneralSection {...ctx} />,
});
