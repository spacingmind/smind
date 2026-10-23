import { Button } from "@/components/ui/button";
import { Icons } from "@/lib/icons";

/** The Settings section id of Settings -> GitHub (github-section.tsx registers it; deep links use `smind:open-settings`). */
export const FORGE_SETTINGS_SECTION_ID = "github";

/** The typed codes whose fix lives in Settings -> GitHub: `gh` missing on the daemon host, or not signed in. */
export function isForgeSetupCode(code: string | undefined): boolean {
  return code === "forge_cli_missing" || code === "forge_unauthenticated";
}

/** Opens Settings -> GitHub through the app's one deep-link event (App.tsx listens). */
export function openForgeSettings(): void {
  window.dispatchEvent(new CustomEvent("smind:open-settings", { detail: { sectionId: FORGE_SETTINGS_SECTION_ID } }));
}

/**
 * The shared "go fix it" link next to every forge setup sentence (ADR-0039 §8): a quiet inline button that opens
 * Settings -> GitHub. `onNavigate` runs first so a host (a dialog) can close itself.
 */
export function ForgeSettingsLink({
  label = "Open GitHub settings",
  onNavigate,
  testId = "forge-settings-link",
}: {
  label?: string;
  onNavigate?: () => void;
  testId?: string;
}) {
  return (
    <Button
      type="button"
      variant="link"
      size="sm"
      className="h-auto p-0 text-ui-sm"
      data-testid={testId}
      onClick={() => {
        onNavigate?.();
        openForgeSettings();
      }}
    >
      {label}
      <Icons.arrowRight aria-hidden className="size-3" />
    </Button>
  );
}
