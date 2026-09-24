import { useEffect, useState } from "react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { useDefaultRunPreferences } from "@/hooks/use-default-run-preferences";
import type { ApprovalPolicy, Provider, ProviderInfo, ProviderListResult } from "@/lib/types";

const APPROVAL_POLICIES: { id: ApprovalPolicy; label: string }[] = [
  { id: "manual", label: "Manual approval" },
  { id: "auto-safe", label: "Auto-safe" },
];

/**
 * Item 13's General section: the composer's two defaults
 * (`hooks/use-default-run-preferences.ts` -- read by the composer itself,
 * Track B's file, not this one). The out-of-tab notifications control that
 * used to live here moved to its own Notifications section
 * (notifications-section.tsx) per the web-sidebar-attention plan's AC3.
 *
 * "No preference" (both selects' first, unlabeled option) is a real,
 * distinct state from picking a provider/policy: it means "let the
 * composer choose its own default", not "always use provider #1" --
 * clearing back to it must be possible, so it's a selectable option, not
 * just the initial state.
 */
export function GeneralSection({ client }: SettingsSectionContext) {
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const { defaultProvider, setDefaultProvider, defaultApprovalPolicy, setDefaultApprovalPolicy } =
    useDefaultRunPreferences();

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (!cancelled) setProviders(result?.providers ?? []);
      })
      .catch((err) => console.error("provider.list failed, hiding the default-provider picker's options", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-general">
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-sm font-medium text-foreground">Defaults for new tasks</h3>
        <label className="flex items-center justify-between gap-4">
          <span className="text-ui-sm text-foreground">Provider</span>
          <select
            aria-label="Default provider"
            data-testid="settings-default-provider"
            value={defaultProvider ?? ""}
            onChange={(e) => setDefaultProvider((e.target.value || null) as Provider | null)}
            className="h-8 shrink-0 rounded-lg border border-input bg-transparent px-2 text-ui-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <option value="">No preference</option>
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label ?? p.id}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center justify-between gap-4">
          <span className="text-ui-sm text-foreground">Approval policy</span>
          <select
            aria-label="Default approval policy"
            data-testid="settings-default-approval-policy"
            value={defaultApprovalPolicy ?? ""}
            onChange={(e) => setDefaultApprovalPolicy((e.target.value || null) as ApprovalPolicy | null)}
            className="h-8 shrink-0 rounded-lg border border-input bg-transparent px-2 text-ui-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <option value="">No preference</option>
            {APPROVAL_POLICIES.map((p) => (
              <option key={p.id} value={p.id}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
      </section>
    </div>
  );
}

registerSettingsSection({
  id: "general",
  label: "General",
  order: 200,
  render: (ctx) => <GeneralSection client={ctx.client} />,
});
