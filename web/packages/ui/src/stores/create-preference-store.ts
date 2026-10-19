import { create, type StoreApi, type UseBoundStore } from "zustand";

/**
 * One persisted UI preference as a shared zustand store (ADR-0032 decision 2).
 *
 * Several components used to each hold their own `useState(read())` copy of the same preference, so a
 * change made in one (Settings) never reached another already mounted (the composer, the sidebar). One
 * store means one copy. Persistence stays where it already was: `read` and `write` are the pure,
 * validating helpers in `lib/` (`lib/storage.ts` etc.), so the storage keys and shapes do not change and
 * a stale or corrupt value still falls back instead of throwing.
 *
 * `read()` runs once, when the store is created; `reload()` re-reads storage (tests, and any future
 * cross-tab sync). Every store registers itself so the test setup can reload them all between tests.
 */
export interface PreferenceState<T> {
  value: T;
  set: (next: T) => void;
  /** Re-reads the persisted value (and replaces the in-memory one). */
  reload: () => void;
}

const registry = new Set<() => void>();

export function createPreferenceStore<T>(read: () => T, write: (value: T) => void): UseBoundStore<StoreApi<PreferenceState<T>>> {
  const store = create<PreferenceState<T>>()((set) => ({
    value: read(),
    set: (next) => {
      write(next);
      set({ value: next });
    },
    reload: () => set({ value: read() }),
  }));
  registry.add(() => store.getState().reload());
  return store;
}

/** Test support: re-reads every preference store from storage (call after clearing it). */
export function reloadAllPreferenceStores(): void {
  for (const reload of registry) reload();
}
