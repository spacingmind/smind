import { useEffect } from "react";

import { useClearProviderCooldown } from "@/hooks/use-clear-provider-cooldown";
import { CLEAR_COOLDOWN_EVENT } from "@/lib/gate-events";

/**
 * Headless: runs the picker gate's "Clear" control (lib/gate-events.ts). It is mounted once inside the daemon query
 * provider, so every picker (composer, home landing, Settings forms) clears through the same `provider.clearCooldown`
 * call, which invalidates provider.list on success; a failure leaves the gate in place and the picker shows it still.
 */
export function GateActionBridge(): null {
  const { clear } = useClearProviderCooldown();
  useEffect(() => {
    const onClear = (e: Event) => {
      const provider = (e as CustomEvent<{ provider?: string }>).detail?.provider;
      if (provider) {
        void clear(provider).catch(() => {});
      }
    };
    window.addEventListener(CLEAR_COOLDOWN_EVENT, onClear);
    return () => window.removeEventListener(CLEAR_COOLDOWN_EVENT, onClear);
  }, [clear]);
  return null;
}
