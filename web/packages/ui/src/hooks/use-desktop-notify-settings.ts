import { useCallback, useEffect, useState } from "react";

import { desktop, isDesktop, type AppSettings } from "@/lib/platform";

/**
 * The desktop Notifications section's view of `app-settings.json` (DT5 B
 * AC8): reads it once on mount and patches it through `appSettingsSet`,
 * storing whatever Rust returns. In a browser build nothing is ever called
 * and `settings` stays null.
 */
export function useDesktopNotifySettings(): {
  settings: AppSettings | null;
  error: string | null;
  set: (patch: Partial<AppSettings>) => Promise<void>;
} {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isDesktop) return;
    let cancelled = false;
    // The call sits inside the promise chain so a missing command in an
    // older shell surfaces as a caught rejection, not a thrown TypeError.
    Promise.resolve()
      .then(() => desktop.appSettingsGet())
      .then((loaded) => {
        if (!cancelled) setSettings(loaded);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const set = useCallback(async (patch: Partial<AppSettings>) => {
    try {
      setSettings(await desktop.appSettingsSet(patch));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  return { settings, error, set };
}
