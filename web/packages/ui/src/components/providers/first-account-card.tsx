import type { JSX } from "react";

import { credentialProviders } from "@/components/accounts-dialog";
import { Button } from "@/components/ui/button";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import type { ProviderInfo } from "@/lib/types";

/**
 * The empty state's first-account card (AC-A5): one primary button per
 * credential provider -- requestConnect does the rest (the detail pane's
 * connect form starts or shows for that provider). CLI providers get only
 * a muted line: they sign in through their own CLI.
 */
export function FirstAccountCard({ providers }: { providers: ProviderInfo[] }): JSX.Element {
  const requestConnect = useProvidersViewStore((s) => s.requestConnect);
  return (
    <div data-testid="first-account-card" className="grid gap-3 rounded-xl border bg-muted/40 p-6">
      <h4 className="text-ui-lg font-medium">Connect your first account</h4>
      <p className="text-ui-base text-muted-foreground">
        Accounts let sMind route the /v1 proxy across your subscriptions and keys.
      </p>
      <div className="flex flex-wrap gap-2">
        {credentialProviders(providers).map((p) => (
          <Button
            key={p.id}
            size="sm"
            data-testid={`first-account-connect-${p.id}`}
            onClick={() => requestConnect(p.id)}
          >
            {p.credentialKind === "oauth" ? `Connect ${p.label}` : `Add ${p.label} key`}
          </Button>
        ))}
      </div>
      {providers
        .filter((p) => p.kind === "cli")
        .map((p) => (
          <p key={p.id} className="text-ui-sm text-muted-foreground">
            {`${p.label ?? p.id} signs in through its own CLI.`}
          </p>
        ))}
    </div>
  );
}
