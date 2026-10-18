import { create } from "zustand";
import type { AccountFilter } from "@/lib/providers-view";

interface ProvidersViewState {
  selectedProviderId: string | null;
  mobileView: "list" | "detail";
  query: string;
  filter: AccountFilter;
  sort: "name" | "status";
  connectProviderId: string | null;
  select(id: string): void;
  back(): void;
  setQuery(q: string): void;
  setFilter(f: AccountFilter): void;
  setSort(s: "name" | "status"): void;
  requestConnect(id: string): void;
  clearConnect(): void;
  reset(): void;
}

const initial = {
  selectedProviderId: null as string | null,
  mobileView: "list" as "list" | "detail",
  query: "",
  filter: "all" as AccountFilter,
  sort: "status" as "name" | "status",
  connectProviderId: null as string | null,
};

export const useProvidersViewStore = create<ProvidersViewState>()((set) => ({
  ...initial,
  select: (id) => set({ selectedProviderId: id, mobileView: "detail", query: "" }),
  back: () => set({ mobileView: "list" }),
  setQuery: (query) => set({ query }),
  setFilter: (filter) => set({ filter }),
  setSort: (sort) => set({ sort }),
  requestConnect: (id) => set({ selectedProviderId: id, mobileView: "detail", query: "", connectProviderId: id }),
  clearConnect: () => set({ connectProviderId: null }),
  reset: () => set({ ...initial }),
}));

export function selectProvider(id: string): void {
  useProvidersViewStore.getState().select(id);
}
