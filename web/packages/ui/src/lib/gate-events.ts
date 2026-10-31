// The picker gate's "Clear cooldown" control sits several component layers below the shell (composer toolbar, home
// landing, Settings forms) with no daemon context of its own in many tests, so it asks the shell by window event, the
// same pattern as `smind:open-settings`. App.tsx owns the one handler that calls provider.clearCooldown.

export const CLEAR_COOLDOWN_EVENT = "smind:clear-provider-cooldown";

export function requestClearCooldown(providerId: string): void {
  window.dispatchEvent(new CustomEvent(CLEAR_COOLDOWN_EVENT, { detail: { provider: providerId } }));
}
