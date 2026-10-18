import { useStore } from "zustand";
import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { LoginLocationOverride } from "@/lib/login-remote";

// ADR-0033: the user's remembered answer to "where do you want to sign
// in?" for each saved connection -- persisted per connection id so a
// remote daemon's sign-in doesn't re-ask every time.

export const LOGIN_LOCATION_STORAGE_KEY = "smind:login-location";

interface LoginLocationState {
  overrides: Record<string, LoginLocationOverride>;
  setOverride: (connectionId: string, value: LoginLocationOverride | null) => void;
}

export const useLoginLocationStore = create<LoginLocationState>()(
  persist(
    (set) => ({
      overrides: {},
      setOverride: (connectionId, value) =>
        set((state) => {
          const overrides = { ...state.overrides };
          if (value === null) delete overrides[connectionId];
          else overrides[connectionId] = value;
          return { overrides };
        }),
    }),
    {
      name: LOGIN_LOCATION_STORAGE_KEY,
      partialize: (state) => ({ overrides: state.overrides }),
    },
  ),
);

export function useLoginLocationOverride(connectionId: string): LoginLocationOverride | null {
  return useStore(useLoginLocationStore, (state) => state.overrides[connectionId] ?? null);
}
