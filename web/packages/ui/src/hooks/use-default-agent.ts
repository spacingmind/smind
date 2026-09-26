import { useCallback, useState } from "react";

import { readStoredDefaultAgentId, writeStoredDefaultAgentId } from "@/lib/settings-preferences";

/**
 * The run-config IA plan's ★ default agent: Settings -> Agents sets it,
 * run-config-toolbar.tsx reads it to seed a brand-new task's toolbar. Same
 * lazy-initial-state shape as the old useDefaultRunPreferences this
 * replaces -- reads once per mount, so a change made in one open settings
 * screen doesn't retroactively apply to a composer already mounted before
 * that change (matching how every other default in this app behaves).
 */
export function useDefaultAgentId(): {
  defaultAgentId: string | null;
  setDefaultAgentId: (id: string | null) => void;
} {
  const [defaultAgentId, setDefaultAgentIdState] = useState<string | null>(readStoredDefaultAgentId);

  const setDefaultAgentId = useCallback((id: string | null) => {
    setDefaultAgentIdState(id);
    writeStoredDefaultAgentId(id);
  }, []);

  return { defaultAgentId, setDefaultAgentId };
}
