// DT5 B AC8: the desktop build's Notifications section -- consent, the one
// test banner that triggers the OS prompt, and the three per-kind switches.
// `@/lib/platform` is mocked per scenario (module-load constants) and the
// section re-imported fresh, same as daemon-section.test.tsx.
import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

function baseSettings(overrides: Record<string, unknown> = {}) {
  return {
    onboardingCompleted: true,
    manageLocalDaemon: true,
    notificationsOn: null as boolean | null,
    notifyPermission: true,
    notifyQuestion: true,
    notifyFailure: true,
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

async function renderSection(opts: { desktop?: boolean; settings?: Record<string, unknown> } = {}) {
  const build = opts.desktop ?? true;
  const stored = opts.settings ?? baseSettings();
  let current = { ...stored };
  const api = {
    appSettingsGet: vi.fn(() => Promise.resolve(current)),
    appSettingsSet: vi.fn((patch: Record<string, unknown>) => {
      // Rust stores a patch and returns what is now stored.
      current = { ...current, ...patch };
      return Promise.resolve(current);
    }),
    notifyTest: vi.fn(() => Promise.resolve()),
  };
  vi.doMock("@/lib/platform", () => ({
    isDesktop: build,
    desktop: build ? api : {},
  }));
  const { listSettingsSections } = await import("@/components/settings/settings-registry");
  await import("@/components/settings/notifications-section");
  const section = listSettingsSections().find((s) => s.id === "notifications");
  if (!section) throw new Error("notifications section did not register");
  render(<>{section.render({ client: null })}</>);
  await flush();
  return api;
}

describe("notifications-section (desktop build)", () => {
  it("shows the consent action and disables the switches before consent", async () => {
    await renderSection({ settings: baseSettings({ notificationsOn: null }) });

    expect(screen.getByTestId("desktop-notifications-group")).toBeInTheDocument();
    expect(screen.getByText("Desktop notifications are off")).toBeInTheDocument();
    expect(screen.getByTestId("desktop-notify-enable")).toHaveTextContent("Turn on desktop notifications");
    expect(screen.queryByTestId("desktop-notify-test")).not.toBeInTheDocument();
    expect(screen.queryByTestId("desktop-notify-disable")).not.toBeInTheDocument();
    for (const id of ["desktop-notify-permission", "desktop-notify-question", "desktop-notify-failure"]) {
      expect(screen.getByTestId(id)).toBeDisabled();
      expect(screen.getByTestId(id)).toHaveAttribute("aria-checked", "true");
    }
    expect(screen.getByTestId("desktop-notify-kinds-hint")).toHaveTextContent("Turn desktop notifications on first.");
  });

  it("turning on records consent and sends the test notification", async () => {
    const api = await renderSection({ settings: baseSettings({ notificationsOn: null }) });

    fireEvent.click(screen.getByTestId("desktop-notify-enable"));
    await flush();
    await flush();

    expect(api.appSettingsSet).toHaveBeenCalledWith({ notificationsOn: true });
    expect(api.notifyTest).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Desktop notifications are on")).toBeInTheDocument();
    expect(screen.getByTestId("desktop-notify-test")).toBeEnabled();
    expect(screen.getByTestId("desktop-notify-disable")).toBeEnabled();
    expect(screen.queryByTestId("desktop-notify-enable")).not.toBeInTheDocument();
  });

  it("a failing test notification shows its error and keeps consent", async () => {
    const api = await renderSection({ settings: baseSettings({ notificationsOn: null }) });
    api.notifyTest.mockRejectedValue(new Error("notifications are not permitted for sMind"));

    fireEvent.click(screen.getByTestId("desktop-notify-enable"));
    await flush();
    await flush();

    expect(api.appSettingsSet).toHaveBeenCalledWith({ notificationsOn: true });
    expect(screen.getByTestId("desktop-notify-error")).toHaveTextContent("notifications are not permitted for sMind");
    // Consent is recorded regardless: the on-row's actions are showing.
    expect(screen.getByText("Desktop notifications are on")).toBeInTheDocument();
    expect(screen.getByTestId("desktop-notify-disable")).toBeInTheDocument();
    expect(screen.queryByTestId("desktop-notify-enable")).not.toBeInTheDocument();
  });

  it("each switch patches only its own field", async () => {
    const api = await renderSection({ settings: baseSettings({ notificationsOn: true }) });

    fireEvent.click(screen.getByTestId("desktop-notify-permission"));
    await flush();
    fireEvent.click(screen.getByTestId("desktop-notify-question"));
    await flush();
    fireEvent.click(screen.getByTestId("desktop-notify-failure"));
    await flush();

    expect(api.appSettingsSet.mock.calls).toEqual([
      [{ notifyPermission: false }],
      [{ notifyQuestion: false }],
      [{ notifyFailure: false }],
    ]);
    expect(screen.getByTestId("desktop-notify-permission")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("desktop-notify-question")).toHaveAttribute("aria-checked", "false");
    expect(screen.getByTestId("desktop-notify-failure")).toHaveAttribute("aria-checked", "false");
  });

  it("a stored false offers to turn on again", async () => {
    await renderSection({ settings: baseSettings({ notificationsOn: false }) });

    expect(screen.getByText("Desktop notifications are off")).toBeInTheDocument();
    expect(screen.getByTestId("desktop-notify-enable")).toHaveTextContent("Turn on desktop notifications");
    for (const id of ["desktop-notify-permission", "desktop-notify-question", "desktop-notify-failure"]) {
      expect(screen.getByTestId(id)).toBeDisabled();
    }
  });

  it("the browser build shows no desktop group", async () => {
    const api = await renderSection({ desktop: false, settings: baseSettings() });

    expect(screen.queryByTestId("desktop-notifications-group")).not.toBeInTheDocument();
    // The browser groups render exactly as before, and nothing desktop is called.
    expect(screen.getByTestId("settings-notifications-toggle")).toBeInTheDocument();
    expect(screen.getByTestId("settings-notifications-sound-toggle")).toBeInTheDocument();
    expect(api.appSettingsGet).not.toHaveBeenCalled();
  });
});
