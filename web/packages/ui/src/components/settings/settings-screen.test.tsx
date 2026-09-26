import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { readStoredThemePreference } from "@/lib/theme";
import { FakeWsClient } from "@/test/fake-ws-client";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderScreen(client = new FakeWsClient(), initialSectionId?: string) {
  render(
    <SettingsScreen
      client={client as never}
      onNavigateBack={() => {}}
      initialSectionId={initialSectionId}
    />,
  );
  return client;
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

describe("SettingsScreen shell", () => {
  it("renders the built-in General and Appearance sections, General active by default (nav regroup puts it first)", () => {
    renderScreen();

    expect(screen.getByTestId("settings-nav-appearance")).toBeInTheDocument();
    expect(screen.getByTestId("settings-nav-general")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("groups the nav per the run-config IA regroup: Agents & providers, and Connection (desktop only)", () => {
    renderScreen();

    // Agents & providers group: heading once, then its two members.
    expect(screen.getAllByText("Agents & providers").length).toBe(1);
    expect(screen.getByTestId("settings-nav-agents")).toHaveTextContent("Agents");
    expect(screen.getByTestId("settings-nav-providers")).toHaveTextContent("Providers");

    // Connection is desktop-only; in a web test neither entry renders.
    expect(screen.queryByText("Connection")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-nav-connections")).not.toBeInTheDocument();
    expect(screen.queryByTestId("settings-nav-daemon")).not.toBeInTheDocument();

    // Order: General · Appearance · Agents & providers · Notifications ·
    // Shortcuts (desktop inserts Connection between the group and
    // Notifications).
    const ids = screen
      .getAllByRole("listitem")
      .map((li) => li.querySelector("[data-testid^='settings-nav-']")?.getAttribute("data-testid"))
      .filter(Boolean);
    expect(ids).toEqual([
      "settings-nav-general",
      "settings-nav-appearance",
      "settings-nav-agents",
      "settings-nav-providers",
      "settings-nav-notifications",
      "settings-nav-shortcuts",
    ]);
  });

  it("the Providers nav stub dispatches smind:open-accounts (no section on this branch; ADR-0015 replaces it)", () => {
    renderScreen();
    const listener = vi.fn();
    window.addEventListener("smind:open-accounts", listener);

    fireEvent.click(screen.getByTestId("settings-nav-providers"));
    expect(listener).toHaveBeenCalledTimes(1);
    window.removeEventListener("smind:open-accounts", listener);
  });

  it("gives the active and inactive nav items the same text-ui-* size (tailwind-merge font-size group regression, see lib/utils.ts's cn() comment)", () => {
    renderScreen();

    const active = screen.getByTestId("settings-nav-appearance");
    const inactive = screen.getByTestId("settings-nav-general");
    const sizeOf = (el: HTMLElement) => el.className.match(/\btext-ui-(?:xl|lg|base|caption|sm|xs)\b/)?.[0];

    expect(sizeOf(active)).toBeDefined();
    expect(sizeOf(active)).toBe(sizeOf(inactive));
  });

  it("switches the detail pane when a different section is clicked", async () => {
    renderScreen();

    fireEvent.click(screen.getByTestId("settings-nav-general"));
    await flush();

    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("is a full-pane screen, not a Dialog -- nothing is portalled and there is no dialog role", () => {
    renderScreen();

    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("Escape and the Back button both navigate back to the previous view", () => {
    const onNavigateBack = vi.fn();
    render(<SettingsScreen client={null} onNavigateBack={onNavigateBack} />);

    fireEvent.click(screen.getByTestId("settings-back-button"));
    expect(onNavigateBack).toHaveBeenCalledTimes(1);

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onNavigateBack).toHaveBeenCalledTimes(2);
  });

  it("initialSectionId opens on that section instead of the first registered one", () => {
    render(<SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="shortcuts" />);

    expect(screen.getByTestId("settings-section-shortcuts")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-appearance")).not.toBeInTheDocument();
  });

  it("an unknown initialSectionId falls back to the first registered section instead of blanking the screen", () => {
    render(<SettingsScreen client={null} onNavigateBack={() => {}} initialSectionId="does-not-exist" />);

    expect(screen.getByTestId("settings-section-general")).toBeInTheDocument();
  });

  it("registering a new section in the test makes it appear without editing the shell", () => {
    const unregister = registerSettingsSection({
      id: "test-only",
      label: "Test Only",
      order: 999,
      render: () => <div data-testid="settings-section-test-only">hi</div>,
    });

    try {
      renderScreen();
      expect(screen.getByTestId("settings-nav-test-only")).toBeInTheDocument();

      fireEvent.click(screen.getByTestId("settings-nav-test-only"));
      expect(screen.getByTestId("settings-section-test-only")).toBeInTheDocument();
    } finally {
      unregister();
    }
  });
});

describe("SettingsScreen Appearance section", () => {
  it("changing the theme updates the document and persists", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-theme-dark"));

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(readStoredThemePreference()).toBe("dark");
  });

  it("changing a font-size axis updates its CSS custom property and persists", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-interface-large"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1.15");

    // Persisted across a remount.
    const client2 = new FakeWsClient();
    render(<SettingsScreen client={client2 as never} onNavigateBack={() => {}} initialSectionId="appearance" />);
    expect(screen.getAllByTestId("settings-font-size-interface-large")[1]).toHaveAttribute("aria-pressed", "true");
  });

  it("each font-size axis is independent -- changing content does not touch interface", () => {
    renderScreen(undefined, "appearance");
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-content-small"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-content")).toBe("0.9");
    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1");
  });
});

describe("SettingsScreen General section", () => {
  // The old default-provider/default-approval-policy controls moved to
  // Settings -> Agents' ★ default agent (run-config IA plan) -- General
  // keeps a pointer rather than going blank or landing on the next
  // section silently. profiles-section.test.tsx covers the ★ control
  // itself.
  it("points to Settings -> Agents instead of the removed default-provider/policy controls", () => {
    renderScreen();
    fireEvent.click(screen.getByTestId("settings-nav-general"));

    expect(screen.getByTestId("settings-section-general")).toHaveTextContent("Settings → Agents");
    expect(screen.queryByTestId("settings-default-provider")).not.toBeInTheDocument();
  });
});

describe("SettingsScreen Notifications section (AC3)", () => {
  function openNotifications(client = new FakeWsClient()) {
    renderScreen(client);
    fireEvent.click(screen.getByTestId("settings-nav-notifications"));
    return client;
  }

  /** jsdom has no Notification API -- installs a minimal fake so the toggle's "default" (clickable) state is reachable at all; without it useNotificationPermission reports "unsupported" and the button stays disabled, which a separate test below covers directly. */
  function installFakeNotification(initialPermission: NotificationPermission) {
    const requestPermission = vi.fn<() => Promise<NotificationPermission>>().mockResolvedValue("granted");
    const constructed: { title: string; options?: NotificationOptions }[] = [];
    class FakeNotification {
      static permission: NotificationPermission = initialPermission;
      static requestPermission = requestPermission;
      constructor(title: string, options?: NotificationOptions) {
        constructed.push({ title, options });
      }
    }
    vi.stubGlobal("Notification", FakeNotification);
    return { requestPermission, constructed };
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("moved here, not duplicated -- General no longer renders the notifications toggle", () => {
    renderScreen();
    fireEvent.click(screen.getByTestId("settings-nav-general"));
    expect(screen.queryByTestId("settings-notifications-toggle")).not.toBeInTheDocument();
  });

  it("never requests Notification permission on mount -- only an explicit click on the toggle does", async () => {
    const { requestPermission } = installFakeNotification("default");
    openNotifications();

    expect(requestPermission).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("settings-notifications-toggle"));
    await flush();

    expect(requestPermission).toHaveBeenCalledTimes(1);
  });

  it("without a Notification API at all, the toggle renders disabled instead of throwing", () => {
    openNotifications();

    const button = screen.getByTestId("settings-notifications-toggle");
    expect(button).toBeDisabled();
    expect(() => fireEvent.click(button)).not.toThrow();
  });

  it("the sound toggle defaults off and persists a click", () => {
    installFakeNotification("granted");
    openNotifications();

    const toggle = screen.getByTestId("settings-notifications-sound-toggle");
    expect(toggle).toHaveAttribute("aria-checked", "false");

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute("aria-checked", "true");
  });

  it("the test-notification button is disabled until permission is granted, then sends one and shows success", async () => {
    installFakeNotification("default");
    openNotifications();
    expect(screen.getByTestId("settings-notifications-test-button")).toBeDisabled();

    fireEvent.click(screen.getByTestId("settings-notifications-toggle"));
    await flush();

    const testButton = screen.getByTestId("settings-notifications-test-button");
    expect(testButton).toBeEnabled();
    fireEvent.click(testButton);

    expect(await screen.findByTestId("settings-notifications-test-success")).toBeInTheDocument();
  });

  it("a Notification constructor that throws shows the failed state instead of crashing", async () => {
    class ThrowingNotification {
      static permission: NotificationPermission = "granted";
      static requestPermission = vi.fn();
      constructor() {
        throw new Error("blocked by platform policy");
      }
    }
    vi.stubGlobal("Notification", ThrowingNotification);
    openNotifications();

    fireEvent.click(screen.getByTestId("settings-notifications-test-button"));

    expect(await screen.findByTestId("settings-notifications-test-error")).toBeInTheDocument();
  });
});
