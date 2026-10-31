import { useState } from "react";

import { AddFromRecipe } from "@/components/providers/add-from-recipe";
import { AddProviderInstance } from "@/components/providers/add-provider-instance";
import { ProvidersScreen } from "@/components/providers/providers-screen";
import {
  registerSettingsSection,
  type SettingsSectionContext,
} from "@/components/settings/settings-registry";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { credentialProviders } from "@/components/accounts-dialog";
import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProvidersViewStore } from "@/stores/providers-view-store";
export { groupAccountsByProvider, removeWarning, providerConsumer } from "@/lib/providers-groups";
export type { ProviderGroup, ProviderConsumer } from "@/lib/providers-groups";

/**
 * Settings -> Providers: the master/detail screen (plan
 * docs/plans/active/providers-login-and-tab.md, AC-A1..A11) -- a
 * provider nav on the left, a detail pane on the right. The connect/
 * rename/credential/remove flows move into the detail pane.
 */
function ProvidersSection({ client }: SettingsSectionContext) {
  const { providersOrFallback: providers } = useProviderCatalog();
  const selectedProviderId = useProvidersViewStore((s) => s.selectedProviderId);
  const requestConnect = useProvidersViewStore((s) => s.requestConnect);
  const [addOpen, setAddOpen] = useState(false);
  // The two Add-* panels are exclusive: opening one closes the other, so the title area never stacks two forms.
  const [recipeOpen, setRecipeOpen] = useState(false);
  // Connect the selected provider when it can connect; the other bucket, a
  // CLI provider or no selection fall back to the first credential provider.
  const credentials = credentialProviders(providers);
  const target = credentials.find((p) => p.id === selectedProviderId) ?? credentials[0];
  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-providers">
      <SettingsSectionTitle
        title="Providers"
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              data-testid="providers-add-instance-toggle"
              aria-expanded={addOpen}
              onClick={() => {
                setRecipeOpen(false);
                setAddOpen((v) => !v);
              }}
            >
              <Icons.providerGeneric aria-hidden className="size-3.5" />
              Add instance
            </Button>
            <Button
              variant="outline"
              size="sm"
              data-testid="providers-add-recipe-toggle"
              aria-expanded={recipeOpen}
              onClick={() => {
                setAddOpen(false);
                setRecipeOpen((v) => !v);
              }}
            >
              <Icons.recipe aria-hidden className="size-3.5" />
              Add from recipe
            </Button>
            <Button
              variant="outline"
              size="sm"
              data-testid="providers-connect-toggle"
              disabled={!target}
              onClick={() => target && requestConnect(target.id)}
            >
              <Icons.add aria-hidden className="size-3.5" />
              Connect account
            </Button>
          </div>
        }
      />
      {addOpen ? <AddProviderInstance hasClient={client !== null} /> : null}
      {recipeOpen ? <AddFromRecipe hasClient={client !== null} onClose={() => setRecipeOpen(false)} /> : null}
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
  description: "Connect provider accounts and add extra Claude Code or Codex logins.",
  order: 180,
  icon: <Icons.credentials aria-hidden className="size-3.5" />,
  render: (ctx) => <ProvidersSection {...ctx} />,
});
