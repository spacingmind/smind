import type { JSX } from "react";
import type { ProviderNavItem } from "@/lib/providers-view";
import { ProviderGlyph } from "@/components/composer/provider-glyph";
import type { WsClient } from "@/lib/ws-client";

const KIND_LABEL: Record<ProviderNavItem["kind"], string> = {
  oauth: "OAuth account",
  "api-key": "API key account",
  cli: "Managed by its own CLI",
  other: "Other accounts",
};

/** Stub: a later task fills in the account list, connect form and CLI hint. */
export function ProviderDetail({ item }: { item: ProviderNavItem | null; client: WsClient | null }): JSX.Element {
  if (!item) {
    return (
      <div data-testid="provider-detail" className="p-6">
        <p className="text-ui-base text-muted-foreground">Select a provider</p>
      </div>
    );
  }
  return (
    <div data-testid="provider-detail" className="p-6">
      <div className="flex items-center gap-2">
        {item.id === "other" ? null : <ProviderGlyph provider={item.id} />}
        <h3 className="text-ui-lg font-medium">{item.label}</h3>
      </div>
      <p className="text-ui-base text-muted-foreground">{KIND_LABEL[item.kind]}</p>
    </div>
  );
}
