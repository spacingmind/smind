import { useCallback, useState } from "react";


import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Switch } from "@/components/ui/switch";

import { useNotificationPermission, type NotificationPermissionState } from "@/hooks/use-notification-permission";
import { useNotificationSoundPreference } from "@/hooks/use-notification-sound-preference";
import { playNotificationSound } from "@/lib/notification-sound";
import { Icons } from "@/lib/icons";

/** Mirrors General's former copy for this toggle -- moved here per the web-sidebar-attention plan (AC3), not duplicated: general-section.tsx no longer renders it. */
const PERMISSION_LABEL: Record<NotificationPermissionState, string> = {
  default: "Enable out-of-tab notifications",
  granted: "Notifications enabled",
  denied: "Notifications blocked -- allow them in your browser's site settings",
  unsupported: "Notifications aren't supported in this browser",
};

const PERMISSION_DESCRIPTION: Record<NotificationPermissionState, string> = {
  default: "Get a browser notification when a backgrounded task finishes or needs a decision.",
  granted: "You'll get a browser notification when a backgrounded task finishes or needs a decision.",
  denied: "Notifications were blocked. Allow them in your browser's site settings to re-enable.",
  unsupported: "This browser doesn't support notifications.",
};

type TestNotificationState = { status: "idle" } | { status: "sent" } | { status: "error"; message: string };

/**
 * The Notifications settings section (web-sidebar-attention plan, AC3):
 * the permission toggle that used to live in General (moved, not
 * duplicated -- see PERMISSION_LABEL's comment), a sound toggle, and a
 * send-test-notification button with inline sent/failed feedback --
 * behavior ported from refs/paseo's DesktopNotificationsSection, adapted
 * from that app's native permission APIs to the browser Notification API.
 */
export function NotificationsSection(_ctx: SettingsSectionContext) {
  const { permission, requestPermission } = useNotificationPermission();
  const { enabled: soundEnabled, setEnabled: setSoundEnabled } = useNotificationSoundPreference();
  const [testState, setTestState] = useState<TestNotificationState>({ status: "idle" });

  const sendTestNotification = useCallback(() => {
    if (typeof window === "undefined" || !("Notification" in window)) {
      setTestState({ status: "error", message: "This browser doesn't support notifications." });
      return;
    }
    try {
      new Notification("sMind", { body: "This is what a sMind notification looks like." });
      if (soundEnabled) playNotificationSound();
      setTestState({ status: "sent" });
    } catch (err) {
      setTestState({ status: "error", message: err instanceof Error ? err.message : String(err) });
    }
  }, [soundEnabled]);

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-notifications">
      <SettingsGroup>
        <SettingsRow
          label="Notifications"
          description={PERMISSION_DESCRIPTION[permission]}
          control={
            <Button
              variant="outline"
              size="sm"
              aria-label={PERMISSION_LABEL[permission]}
              data-testid="settings-notifications-toggle"
              disabled={permission !== "default"}
              onClick={requestPermission}
            >
              {permission === "granted" ? "Enabled" : "Enable"}
            </Button>
          }
        />

        <SettingsRow
          label="Play a sound"
          description="Play a short sound alongside a browser notification."
          control={
            <Switch
              checked={soundEnabled}
              onCheckedChange={setSoundEnabled}
              aria-label="Play a sound with notifications"
              data-testid="settings-notifications-sound-toggle"
            />
          }
        />

        <SettingsRow
          label="Test notification"
          description={
            permission === "granted"
              ? "Send a test notification to confirm it works."
              : "Enable notifications above first."
          }
          control={
            <Button
              variant="outline"
              size="sm"
              data-testid="settings-notifications-test-button"
              disabled={permission !== "granted"}
              onClick={sendTestNotification}
            >
              Send test
            </Button>
          }
        />

        {testState.status !== "idle" && (
          <div className="px-4 pb-3">
            {testState.status === "sent" && (
              <p data-testid="settings-notifications-test-success" className="text-ui-base text-success">
                Test notification sent.
              </p>
            )}
            {testState.status === "error" && (
              <FormError message={testState.message} testId="settings-notifications-test-error" />
            )}
          </div>
        )}
      </SettingsGroup>
    </div>
  );
}

registerSettingsSection({
  id: "notifications",
  label: "Notifications",
  description: "Browser notifications and sounds.",
  // Ungrouped, so it sorts with General/Appearance above every group heading.
  order: 110,
  icon: <Icons.notifications aria-hidden className="size-3.5" />,
  render: (ctx) => <NotificationsSection {...ctx} />,
});
