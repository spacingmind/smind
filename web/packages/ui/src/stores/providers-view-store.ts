import { create } from "zustand";
import type { AccountFilter } from "@/lib/providers-view";

interface ProvidersViewState {
  selectedProviderId: string | null;
  mobileView: "list" | "detail";
  query: string;
  filter: AccountFilter;
  sort: "name" | "status";
  connectProviderId: string | null;
  reloginAccountId: number | null;
  select(id: string): void;
  back(): void;
  setQuery(q: string): void;
  setFilter(f: AccountFilter): void;
  setSort(s: "name" | "status"): void;
  requestConnect(id: string): void;
  clearConnect(): void;
  requestRelogin(accountId: number, providerNavId: string): void;
  clearRelogin(): void;
  reset(): void;
}

const initial = {
  selectedProviderId: null as string | null,
  mobileView: "list" as "list" | "detail",
  query: "",
  filter: "all" as AccountFilter,
  sort: "status" as "name" | "status",
  connectProviderId: null as string | null,
  reloginAccountId: null as number | null,
};

export const useProvidersViewStore = create<ProvidersViewState>()((set) => ({
  ...initial,
  select: (id) =>
    set({ selectedProviderId: id, mobileView: "detail", query: "", filter: "all", sort: "status", connectProviderId: null, reloginAccountId: null }),
  back: () => set({ mobileView: "list", connectProviderId: null, reloginAccountId: null }),
  setQuery: (query) => set({ query }),
  setFilter: (filter) => set({ filter }),
  setSort: (sort) => set({ sort }),
  requestConnect: (id) =>
    set({ selectedProviderId: id, mobileView: "detail", query: "", filter: "all", sort: "status", connectProviderId: id, reloginAccountId: null }),
  clearConnect: () => set({ connectProviderId: null }),
  requestRelogin: (accountId, providerNavId) =>
    set({ selectedProviderId: providerNavId, mobileView: "detail", query: "", filter: "all", sort: "status", connectProviderId: null, reloginAccountId: accountId }),
  clearRelogin: () => set({ reloginAccountId: null }),
  reset: () => set({ ...initial }),
}));

export function selectProvider(id: string): void {
  useProvidersViewStore.getState().select(id);
}
