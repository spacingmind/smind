import { useMemo, type JSX } from "react";

import { Icons } from "@/lib/icons";
import { buildProviderNav, type ProviderNavItem } from "@/lib/providers-view";
import type { WsClient } from "@/lib/ws-client";
import { useAccountRows } from "@/hooks/use-account-rows";
import { useIsMobile } from "@/hooks/use-mobile";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { ProviderDetail } from "@/components/providers/provider-detail";
import { ProviderNav } from "@/components/providers/provider-nav";

export function ProvidersScreen({ client }: { client: WsClient | null }): JSX.Element {
  const { providers } = useProviderCatalog();
  const { rows, loaded } = useAccountRows();
  const isMobile = useIsMobile();
  const selectedProviderId = useProvidersViewStore((s) => s.selectedProviderId);
  const mobileView = useProvidersViewStore((s) => s.mobileView);
  const select = useProvidersViewStore((s) => s.select);
  const back = useProvidersViewStore((s) => s.back);

  const items = useMemo<ProviderNavItem[]>(
    () =>
      buildProviderNav({
        providers,
        accounts: rows.map((r) => r.account),
        toneOf: (id) => rows.find((r) => r.account.id === id)?.status.tone,
      }),
    [providers, rows],
  );
  const selected = items.find((item) => item.id === selectedProviderId) ?? items[0] ?? null;

  if (!loaded) {
    return <p className="text-ui-base text-muted-foreground">Loading…</p>;
  }

  if (isMobile) {
    return (
      <div
        data-testid="providers-screen"
        data-layout="pushed"
        className="@container/providers flex min-h-[28rem] overflow-hidden rounded-xl border bg-card"
      >
        {mobileView === "list" ? (
          <div className="w-full p-2">
            <ProviderNav items={items} selectedId={selected?.id ?? null} onSelect={select} mode="full" />
          </div>
        ) : (
          <div className="min-w-0 flex-1">
            <div className="flex h-8 items-center gap-2 px-2">
              <button
                type="button"
                data-testid="provider-detail-back"
                onClick={back}
                className="flex h-8 items-center gap-1.5 rounded-lg px-2 hover:bg-hover focus-visible:ring-2 focus-visible:ring-ring/50 outline-none"
              >
                <Icons.back aria-hidden className="size-4" />
                <span className="text-ui-base">Providers</span>
              </button>
            </div>
            <ProviderDetail item={selected} client={client} />
          </div>
        )}
      </div>
    );
  }

  return (
    <div
      data-testid="providers-screen"
      data-layout="split"
      className="@container/providers flex min-h-[28rem] overflow-hidden rounded-xl border bg-card"
    >
      <div className="hidden w-56 shrink-0 border-r p-2 @2xl/providers:block">
        <ProviderNav items={items} selectedId={selected?.id ?? null} onSelect={select} mode="full" />
      </div>
      <div className="flex w-14 shrink-0 border-r p-2 @2xl/providers:hidden">
        <ProviderNav items={items} selectedId={selected?.id ?? null} onSelect={select} mode="rail" />
      </div>
      <div className="min-w-0 flex-1">
        <ProviderDetail item={selected} client={client} />
      </div>
    </div>
  );
}
