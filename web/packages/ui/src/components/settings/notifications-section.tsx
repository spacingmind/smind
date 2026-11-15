import { useCallback, useState } from "react";


import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Button } from "@/components/ui/button";
import { FormError } from "@/components/ui/form-error";
import { Switch } from "@/components/ui/switch";

import { useDesktopNotifySettings } from "@/hooks/use-desktop-notify-settings";
import { useNotificationPermission, type NotificationPermissionState } from "@/hooks/use-notification-permission";
import { useNotificationSoundPreference } from "@/hooks/use-notification-sound-preference";
import { desktop, isDesktop } from "@/lib/platform";
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
  // `isDesktop` is a build-time constant, so exactly one of the two ever
  // mounts and each owns its hooks.
  return isDesktop ? <DesktopNotificationsSection /> : <BrowserNotificationsSection />;
}

function BrowserNotificationsSection() {
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

/**
 * The desktop build's Notifications section (DT5 B AC8): the app's own
 * consent switch plus the three per-kind switches, all persisted in
 * `app-settings.json`. Nothing about a notification's content is ever
 * shown here -- only consent and kinds.
 */
function DesktopNotificationsSection() {
  const { settings, error, set } = useDesktopNotifySettings();
  const [testError, setTestError] = useState<string | null>(null);
  const on = settings?.notificationsOn === true;

  const sendTest = useCallback(async () => {
    setTestError(null);
    try {
      await desktop.notifyTest();
    } catch (err) {
      setTestError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  // Recording consent and asking the OS are one action: the test banner is
  // what makes macOS show its own permission prompt (AC8).
  async function turnOn() {
    await set({ notificationsOn: true });
    await sendTest();
  }

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-notifications">
      <SettingsGroup title="Desktop notifications" testId="desktop-notifications-group">
        {on ? (
          <SettingsRow
            label="Desktop notifications are on"
            description="If nothing appears, check System Settings > Notifications for sMind (the app cannot read that permission back)."
            control={
              <div className="flex gap-2">
                <Button variant="outline" size="sm" data-testid="desktop-notify-test" onClick={() => void sendTest()}>
                  Send a test
                </Button>
                <Button variant="ghost" size="sm" data-testid="desktop-notify-disable" onClick={() => void set({ notificationsOn: false })}>
                  Turn off
                </Button>
              </div>
            }
          />
        ) : (
          <SettingsRow
            label="Desktop notifications are off"
            description="sMind has not asked for notification permission yet. Turning them on sends one test notification, which is when macOS asks for permission."
            control={
              <Button size="sm" data-testid="desktop-notify-enable" onClick={() => void turnOn()}>
                Turn on desktop notifications
              </Button>
            }
          />
        )}

        <SettingsRow
          label="Permission requests"
          description="A run needs your approval."
          control={
            <Switch
              checked={settings?.notifyPermission ?? true}
              onCheckedChange={(checked) => void set({ notifyPermission: checked })}
              aria-label="Permission requests"
              data-testid="desktop-notify-permission"
              disabled={!on}
            />
          }
        />

        <SettingsRow
          label="Questions from agents"
          description="An agent asked you something."
          control={
            <Switch
              checked={settings?.notifyQuestion ?? true}
              onCheckedChange={(checked) => void set({ notifyQuestion: checked })}
              aria-label="Questions from agents"
              data-testid="desktop-notify-question"
              disabled={!on}
            />
          }
        />

        <SettingsRow
          label="Failed runs"
          description="A run ended with an error."
          control={
            <Switch
              checked={settings?.notifyFailure ?? true}
              onCheckedChange={(checked) => void set({ notifyFailure: checked })}
              aria-label="Failed runs"
              data-testid="desktop-notify-failure"
              disabled={!on}
            />
          }
        />

        {!on && (
          <p data-testid="desktop-notify-kinds-hint" className="px-4 py-3 text-ui-sm text-foreground-muted">
            Turn desktop notifications on first.
          </p>
        )}

        {testError && (
          <div className="px-4 pb-3">
            <FormError message={testError} testId="desktop-notify-error" />
          </div>
        )}
        {error && (
          <div className="px-4 pb-3">
            <FormError message={error} testId="desktop-notify-settings-error" />
          </div>
        )}
      </SettingsGroup>
    </div>
  );
}

registerSettingsSection({
  id: "notifications",
  label: "Notifications",
  description: isDesktop ? "When sMind may show a banner for something that needs you." : "Browser notifications and sounds.",
  // Ungrouped, so it sorts with General/Appearance above every group heading.
  order: 110,
  icon: <Icons.notifications aria-hidden className="size-3.5" />,
  render: (ctx) => <NotificationsSection {...ctx} />,
});
