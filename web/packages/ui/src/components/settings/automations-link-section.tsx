import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { useAutomations } from "@/hooks/use-automations";
import { Icons } from "@/lib/icons";

export const AUTOMATIONS_SECTION_ID = "automations";

/**
 * Settings -> Automations since the page moved out of Settings: the page itself is a top-level view now, so this
 * section only shows the daemon's read-only state (the switches are config.yaml edits) and links to the page via
 * the `smind:open-automations` window event App.tsx listens for (same pattern as `smind:open-settings`).
 */
function AutomationsLinkSection({ closeSettings }: SettingsSectionContext) {
  const listing = useAutomations().data;
  const state = !listing?.enabled ? "Automations are off" : listing.paused ? "Automations are paused" : "Automations are on";
  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-automations">
      {listing && (
        <p className="text-ui-base text-foreground" data-testid="settings-automations-state">
          {state}
        </p>
      )}
      <div>
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
      </div>
    </div>
  );
}

registerSettingsSection({
  id: AUTOMATIONS_SECTION_ID,
  label: "Automations",
  groupLabel: "Agents & providers",
  description: "Rules that start a task when a signal fires.",
  order: 179,
  icon: <Icons.automation aria-hidden className="size-3.5" />,
  render: (ctx) => <AutomationsLinkSection {...ctx} />,
});
