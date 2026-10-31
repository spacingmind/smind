import type { JSX } from "react";

import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import type { ProviderInfo } from "@/lib/types";

/** Settings -> Providers: what a recipe-made instance was built from and its caveats. Nothing for a non-recipe provider. */
export function ProviderRecipeInfo({ info }: { info: ProviderInfo }): JSX.Element | null {
  if (!info.recipe) return null;
  return (
    <SettingsGroup testId={`provider-recipe-${info.id}`} title="Recipe">
      <SettingsRow label="Recipe" description={info.recipe} />
      {info.endpointKind ? <SettingsRow label="Endpoint" description={info.endpointKind} /> : null}
      {info.vendor && info.vendor !== "generic" ? <SettingsRow label="Vendor" description={info.vendor} /> : null}
      {info.thinking === "hidden" ? (
        <SettingsRow
          label="Thinking"
          description="Cannot be turned off on this endpoint"
          testId="provider-recipe-thinking-hidden"
        />
      ) : null}
      {info.unverified ? (
        <SettingsRow
          label="Unverified"
          description="This recipe was never verified against a live endpoint."
          testId="provider-recipe-unverified"
        />
      ) : null}
      {info.notes?.length ? (
        <SettingsRow label="Notes">
          <ul className="list-disc pl-4 text-ui-sm text-foreground-muted">
            {info.notes.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </SettingsRow>
      ) : null}
    </SettingsGroup>
  );
}
