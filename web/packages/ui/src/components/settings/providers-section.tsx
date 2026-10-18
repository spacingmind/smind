import { ProvidersScreen } from "@/components/providers/providers-screen";
import {
  registerSettingsSection,
  type SettingsSectionContext,
} from "@/components/settings/settings-registry";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { Icons } from "@/lib/icons";
export { groupAccountsByProvider, removeWarning, providerConsumer } from "@/lib/providers-groups";
export type { ProviderGroup, ProviderConsumer } from "@/lib/providers-groups";

/**
 * Settings -> Providers: the master/detail screen (plan
 * docs/plans/active/providers-login-and-tab.md, AC-A1..A11) -- a
 * provider nav on the left, a detail pane on the right. The connect/
 * rename/credential/remove flows move into the detail pane.
 */
function ProvidersSection({ client }: SettingsSectionContext) {
  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-providers">
      <SettingsSectionTitle title="Providers" />
      <ProvidersScreen client={client} />
    </div>
  );
}

// Nests under "Agents & providers" (run-config IA plan) alongside Agents
// (order 175) -- order 180 keeps it immediately after Agents, matching
// the plan's "sub-items: Agents, Providers" sequence.
registerSettingsSection({
  id: "providers",
  label: "Providers",
  groupLabel: "Agents & providers",
  order: 180,
  icon: <Icons.credentials aria-hidden className="size-3.5" />,
  render: (ctx) => <ProvidersSection {...ctx} />,
});
