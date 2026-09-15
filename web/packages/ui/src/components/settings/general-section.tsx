import { useEffect, useState } from "react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { useDefaultRunPreferences } from "@/hooks/use-default-run-preferences";
import { useNotificationPermission, type NotificationPermissionState } from "@/hooks/use-notification-permission";
import type { ApprovalPolicy, Provider, ProviderInfo, ProviderListResult } from "@/lib/types";

const APPROVAL_POLICIES: { id: ApprovalPolicy; label: string }[] = [
  { id: "manual", label: "Manual approval" },
  { id: "auto-safe", label: "Auto-safe" },
];

/** Mirrors the notifications toggle's former home (app-sidebar.tsx's header bell), moved here per Item 13 -- see that file's history for the pre-move version. */
const NOTIFICATION_LABEL: Record<NotificationPermissionState, string> = {
  default: "Enable out-of-tab notifications",
  granted: "Notifications enabled",
  denied: "Notifications blocked -- allow them in your browser's site settings",
  unsupported: "Notifications aren't supported in this browser",
};

const NOTIFICATION_DESCRIPTION: Record<NotificationPermissionState, string> = {
  default: "Get a browser notification when a backgrounded task finishes or needs a decision.",
  granted: "You'll get a browser notification when a backgrounded task finishes or needs a decision.",
  denied: "Notifications were blocked. Allow them in your browser's site settings to re-enable.",
  unsupported: "This browser doesn't support notifications.",
};

/**
 * Item 13's General section: the composer's two defaults
 * (`hooks/use-default-run-preferences.ts` -- read by the composer itself,
 * Track B's file, not this one) and the out-of-tab notifications control
 * that used to be the sidebar header's bell button.
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
  const { permission, requestPermission } = useNotificationPermission();

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
        <h3 className="text-sm font-medium text-foreground">Defaults for new tasks</h3>
        <label className="flex items-center justify-between gap-4">
          <span className="text-sm text-foreground">Provider</span>
          <select
            aria-label="Default provider"
            data-testid="settings-default-provider"
            value={defaultProvider ?? ""}
            onChange={(e) => setDefaultProvider((e.target.value || null) as Provider | null)}
            className="h-8 shrink-0 rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
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
          <span className="text-sm text-foreground">Approval policy</span>
          <select
            aria-label="Default approval policy"
            data-testid="settings-default-approval-policy"
            value={defaultApprovalPolicy ?? ""}
            onChange={(e) => setDefaultApprovalPolicy((e.target.value || null) as ApprovalPolicy | null)}
            className="h-8 shrink-0 rounded-lg border border-input bg-transparent px-2 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
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

      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium text-foreground">Notifications</h3>
        <div className="flex items-center justify-between gap-4">
          <p className="max-w-sm text-sm text-muted-foreground">{NOTIFICATION_DESCRIPTION[permission]}</p>
          <Button
            variant="outline"
            size="sm"
            aria-label={NOTIFICATION_LABEL[permission]}
            data-testid="settings-notifications-toggle"
            disabled={permission !== "default"}
            onClick={requestPermission}
          >
            {permission === "granted" ? "Enabled" : "Enable"}
          </Button>
        </div>
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
