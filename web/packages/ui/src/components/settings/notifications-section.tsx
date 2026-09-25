import { useCallback, useState } from "react";

import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { useNotificationPermission, type NotificationPermissionState } from "@/hooks/use-notification-permission";
import { useNotificationSoundPreference } from "@/hooks/use-notification-sound-preference";
import { playNotificationSound } from "@/lib/notification-sound";
import { cn } from "@/lib/utils";

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
      new Notification("smind", { body: "This is what a smind notification looks like." });
      if (soundEnabled) playNotificationSound();
      setTestState({ status: "sent" });
    } catch (err) {
      setTestState({ status: "error", message: err instanceof Error ? err.message : String(err) });
    }
  }, [soundEnabled]);

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-notifications">
      <section className="flex flex-col gap-2">
        <h3 className="text-sm font-medium text-foreground">Notifications</h3>
        <div className="flex items-center justify-between gap-4">
          <p className="max-w-sm text-sm text-muted-foreground">{PERMISSION_DESCRIPTION[permission]}</p>
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
        </div>
      </section>

      <section className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <h3 className="text-sm font-medium text-foreground">Play a sound</h3>
          <p className="max-w-sm text-sm text-muted-foreground">
            Play a short sound alongside a browser notification.
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={soundEnabled}
          aria-label="Play a sound with notifications"
          data-testid="settings-notifications-sound-toggle"
          onClick={() => setSoundEnabled(!soundEnabled)}
          className={cn(
            "h-5 w-9 shrink-0 rounded-full border border-input transition-colors",
            soundEnabled ? "bg-primary" : "bg-transparent",
          )}
        >
          <span
            className={cn(
              "block size-3.5 rounded-full bg-background shadow transition-transform",
              soundEnabled ? "translate-x-4" : "translate-x-0.5",
            )}
          />
        </button>
      </section>

      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-4">
          <div className="flex flex-col gap-0.5">
            <h3 className="text-sm font-medium text-foreground">Test notification</h3>
            <p className="max-w-sm text-sm text-muted-foreground">
              {permission === "granted"
                ? "Send a test notification to confirm it works."
                : "Enable notifications above first."}
            </p>
          </div>
          <Button
            variant="outline"
            size="sm"
            data-testid="settings-notifications-test-button"
            disabled={permission !== "granted"}
            onClick={sendTestNotification}
          >
            Send test
          </Button>
        </div>
        {testState.status === "sent" && (
          <p data-testid="settings-notifications-test-success" className="text-sm text-status-success">
            Test notification sent.
          </p>
        )}
        {testState.status === "error" && (
          <p data-testid="settings-notifications-test-error" className="text-sm text-status-danger">
            {testState.message}
          </p>
        )}
      </section>
    </div>
  );
}

registerSettingsSection({
  id: "notifications",
  label: "Notifications",
  order: 250,
  render: (ctx) => <NotificationsSection {...ctx} />,
});
