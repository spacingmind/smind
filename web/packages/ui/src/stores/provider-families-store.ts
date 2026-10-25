import { create } from "zustand";

import type { ProviderInfo } from "@/lib/types";

interface ProviderFamiliesState {
  /** Instance id -> the built-in it extends (claude-native | codex-native), as the catalog last said. */
  extendsById: Readonly<Record<string, string>>;
  /** Replaces the map from a provider.list answer (the catalog hook calls this; nothing else writes it). */
  setFromCatalog(providers: readonly ProviderInfo[]): void;
}

const NONE: Readonly<Record<string, string>> = {};

/**
 * Which built-in an instance extends, so `ProviderIcon` (rendered in pickers, nav, usage rows that only hold an id)
 * can wear the base provider's mark for an instance named anything. Fed by `useProviderCatalog`; a plain zustand
 * store because the icon has no QueryClient (ADR-0032: zustand for client-held derived state).
 */
export const useProviderFamilies = create<ProviderFamiliesState>((set, get) => ({
  extendsById: NONE,
  setFromCatalog(providers) {
    const next: Record<string, string> = {};
    for (const p of providers) if (p.extends) next[p.id] = p.extends;
    const prev = get().extendsById;
    const prevKeys = Object.keys(prev);
    if (prevKeys.length === Object.keys(next).length && prevKeys.every((k) => prev[k] === next[k])) return;
    set({ extendsById: Object.keys(next).length === 0 ? NONE : next });
  },
}));
