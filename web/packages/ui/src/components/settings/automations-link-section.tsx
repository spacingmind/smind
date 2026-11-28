import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { ConfigSwitchRow } from "@/components/settings/config-switch-row";
import { Button } from "@/components/ui/button";
import { useAutomations } from "@/hooks/use-automations";
import { Icons } from "@/lib/icons";

export const AUTOMATIONS_SECTION_ID = "automations";

/**
 * Settings -> Automations since the page moved out of Settings: the page itself is a top-level view now, so this
 * section holds the two daemon switches (config.set, ADR-0062) and links to the page via
 * the `smind:open-automations` window event App.tsx listens for (same pattern as `smind:open-settings`).
 */
function AutomationsLinkSection({ closeSettings }: SettingsSectionContext) {
  const listing = useAutomations().data;
  const state = !listing?.enabled ? "Automations are off" : listing.paused ? "Automations are paused" : "Automations are on";
  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-automations">
      <SettingsGroup>
        <ConfigSwitchRow
          configKey="automations.enabled"
          label="Automations"
          description="Rules that start a task when a signal fires."
          testId="config-automations-enabled"
        />
        <ConfigSwitchRow
          configKey="automations.paused"
          label="Pause automations"
          description="While paused, signals are recorded and nothing fires."
          testId="config-automations-paused"
        />
      </SettingsGroup>
      <SettingsGroup>
        <SettingsRow
          label="Automations"
          description={listing ? <span data-testid="settings-automations-state">{state}</span> : undefined}
          control={
            <Button
              type="button"
              size="sm"
              data-testid="settings-open-automations"
              onClick={() => {
                closeSettings?.();
                window.dispatchEvent(new CustomEvent("smind:open-automations"));
              }}
            >
              Open Automations
            </Button>
          }
        />
      </SettingsGroup>
    </div>
  );
}

registerSettingsSection({
  id: AUTOMATIONS_SECTION_ID,
  label: "Automations",
  groupLabel: "Agents",
  description: "Rules that start a task when a signal fires.",
  order: 180,
  icon: <Icons.automation aria-hidden className="size-3.5" />,
  render: (ctx) => <AutomationsLinkSection {...ctx} />,
});
