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
      version: 1,
      partialize: (state) => ({ overrides: state.overrides }),
      // A corrupt or older persisted shape drops to empty overrides rather
      // than poisoning every login with a bogus location.
      migrate: (persisted) => {
        const overrides = (persisted as { overrides?: unknown } | undefined)?.overrides;
        if (typeof overrides !== "object" || overrides === null || Array.isArray(overrides)) {
          return { overrides: {} };
        }
        const clean: Record<string, LoginLocationOverride> = {};
        for (const [id, value] of Object.entries(overrides as Record<string, unknown>)) {
          if (value === "this-device" || value === "other-device") clean[id] = value;
        }
        return { overrides: clean };
      },
      merge: (persisted, current) => {
        const migrated = (persisted as { overrides?: unknown } | undefined)?.overrides;
        const valid =
          typeof migrated === "object" &&
          migrated !== null &&
          !Array.isArray(migrated) &&
          Object.values(migrated as Record<string, unknown>).every(
            (v) => v === "this-device" || v === "other-device",
          );
        return valid ? { ...current, overrides: migrated as Record<string, LoginLocationOverride> } : current;
      },
    },
  ),
);

export function useLoginLocationOverride(connectionId: string): LoginLocationOverride | null {
  return useStore(useLoginLocationStore, (state) => state.overrides[connectionId] ?? null);
}
