import { useEffect, useMemo, useRef, type JSX } from "react";

import { buildProviderNav, type ProviderNavItem } from "@/lib/providers-view";
import type { WsClient } from "@/lib/ws-client";
import { useAccountRows } from "@/hooks/use-account-rows";
import { useIsMobile } from "@/hooks/use-mobile";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { setSettingsBackGuard } from "@/lib/settings-back-guard";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { FirstAccountCard } from "@/components/providers/first-account-card";
import { ProviderDetail } from "@/components/providers/provider-detail";
import { ProviderNav } from "@/components/providers/provider-nav";

export function ProvidersScreen({ client }: { client: WsClient | null }): JSX.Element {
  const { providersOrFallback: providers, settled } = useProviderCatalog();
  // Leaving Settings Providers leaves no stale selection/connect flags behind.
  useEffect(() => () => useProvidersViewStore.getState().reset(), []);
  const { rows, loaded, error } = useAccountRows();
  const connectProviderId = useProvidersViewStore((s) => s.connectProviderId);
  const isMobile = useIsMobile();
  const selectedProviderId = useProvidersViewStore((s) => s.selectedProviderId);
  const mobileView = useProvidersViewStore((s) => s.mobileView);
  const select = useProvidersViewStore((s) => s.select);

  const items = useMemo<ProviderNavItem[]>(
    () =>
      buildProviderNav({
        providers,
        accounts: rows.map((r) => r.account),
        toneOf: (id) => rows.find((r) => r.account.id === id)?.badge.tone,
      }),
    [providers, rows],
  );
  const selected = items.find((item) => item.id === selectedProviderId) ?? items[0] ?? null;

  // Mobile push/pop moves focus with the view: the detail pane when it
  // opens, the previously selected nav item when it closes.
  const screenRef = useRef<HTMLDivElement>(null);
  const detailRef = useRef<HTMLDivElement>(null);
  const previousView = useRef(mobileView);
  const selectedId = selected?.id ?? null;
  useEffect(() => {
    const previous = previousView.current;
    previousView.current = mobileView;
    if (!isMobile) return;
    if (mobileView === "detail" && previous !== "detail") detailRef.current?.focus();
    if (mobileView === "list" && previous === "detail" && selectedId !== null) {
      screenRef.current?.querySelector<HTMLElement>(`[data-testid="provider-nav-item-${selectedId}"]`)?.focus();
    }
  }, [mobileView, isMobile, selectedId]);

  // On a compact viewport the Settings header's back pops the provider detail first (one back control for the whole screen).
  useEffect(() => {
    if (!isMobile) return;
    return setSettingsBackGuard(() => {
      const state = useProvidersViewStore.getState();
      if (state.mobileView !== "detail") return false;
      state.back();
      return true;
    });
  }, [isMobile]);

  if (!loaded) {
    if (error) {
      return (
        <p role="alert" className="text-ui-base text-destructive">
          {error instanceof Error ? error.message : String(error)}
        </p>
      );
    }
    return <p className="text-ui-base text-muted-foreground">Loading…</p>;
  }

  // The empty state (AC-A5): no accounts at all and neither a provider
  // explicitly selected nor a connect in progress -- the first-account card
  // takes the detail slot (desktop) or sits above the list (mobile) until
  // the user picks a provider or connects one; no nav item is current then.
  const empty = rows.length === 0 && selectedProviderId === null && connectProviderId === null;
  const navSelectedId = empty ? null : (selected?.id ?? null);

  if (isMobile) {
    return (
      <div
        ref={screenRef}
        data-testid="providers-screen"
        data-layout="pushed"
        className="@container/providers flex min-h-[28rem] overflow-hidden rounded-xl border bg-card"
      >
        {mobileView === "list" ? (
          <div className="w-full p-2">
            {empty ? <FirstAccountCard providers={providers} settled={settled} /> : null}
            <ProviderNav items={items} selectedId={navSelectedId} onSelect={select} mode="full" />
          </div>
        ) : (
          <div ref={detailRef} tabIndex={-1} className="min-w-0 flex-1 outline-none">
            {empty ? <FirstAccountCard providers={providers} settled={settled} /> : <ProviderDetail item={selected} client={client} />}
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
        <ProviderNav items={items} selectedId={navSelectedId} onSelect={select} mode="full" />
      </div>
      <div className="flex w-14 shrink-0 border-r p-2 @2xl/providers:hidden">
        <ProviderNav items={items} selectedId={navSelectedId} onSelect={select} mode="rail" />
      </div>
      <div className="min-w-0 flex-1">
        {empty ? <FirstAccountCard providers={providers} settled={settled} /> : <ProviderDetail item={selected} client={client} />}
      </div>
    </div>
  );
}
