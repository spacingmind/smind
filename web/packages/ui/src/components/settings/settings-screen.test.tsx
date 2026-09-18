import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { SettingsScreen } from "@/components/settings/settings-screen";
import { readStoredDefaultApprovalPolicy, readStoredDefaultProvider } from "@/lib/settings-preferences";
import { readStoredThemePreference } from "@/lib/theme";
import { FakeWsClient } from "@/test/fake-ws-client";

const PROVIDERS = {
  providers: [
    { id: "claude-native", label: "Claude Code", credentialKind: "oauth", accountProvider: "anthropic" },
    { id: "glm", label: "GLM", kind: "cli" },
  ],
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderScreen(client = new FakeWsClient()) {
  render(<SettingsScreen client={client as never} onNavigateBack={() => {}} />);
  return client;
}

afterEach(() => {
  window.localStorage.clear();
  document.documentElement.className = "";
  document.documentElement.style.cssText = "";
});

describe("SettingsScreen shell", () => {
  it("renders the built-in Appearance and General sections, Appearance active by default", () => {
    renderScreen();

    expect(screen.getByTestId("settings-nav-appearance")).toBeInTheDocument();
    expect(screen.getByTestId("settings-nav-general")).toBeInTheDocument();
    expect(screen.getByTestId("settings-section-appearance")).toBeInTheDocument();
    expect(screen.queryByTestId("settings-section-general")).not.toBeInTheDocument();
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
    renderScreen();
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-theme-dark"));

    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(readStoredThemePreference()).toBe("dark");
  });

  it("changing a font-size axis updates its CSS custom property and persists", () => {
    renderScreen();
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-interface-large"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1.15");

    // Persisted across a remount.
    const client2 = new FakeWsClient();
    render(<SettingsScreen client={client2 as never} onNavigateBack={() => {}} />);
    expect(screen.getAllByTestId("settings-font-size-interface-large")[1]).toHaveAttribute("aria-pressed", "true");
  });

  it("each font-size axis is independent -- changing content does not touch interface", () => {
    renderScreen();
    const appearance = within(screen.getByTestId("settings-section-appearance"));

    fireEvent.click(appearance.getByTestId("settings-font-size-content-small"));

    expect(document.documentElement.style.getPropertyValue("--font-scale-content")).toBe("0.9");
    expect(document.documentElement.style.getPropertyValue("--font-scale-interface")).toBe("1");
  });
});

describe("SettingsScreen General section", () => {
  function openGeneral(client = new FakeWsClient()) {
    renderScreen(client);
    fireEvent.click(screen.getByTestId("settings-nav-general"));
    return client;
  }

  it("fetches and lists providers for the default-provider picker", async () => {
    const client = openGeneral();
    client.nth("provider.list", 0).resolve(PROVIDERS);
    await flush();

    const select = screen.getByTestId("settings-default-provider");
    expect(within(select).getByText("Claude Code")).toBeInTheDocument();
    expect(within(select).getByText("GLM")).toBeInTheDocument();
  });

  it("persists the chosen default provider and approval policy", async () => {
    const client = openGeneral();
    client.nth("provider.list", 0).resolve(PROVIDERS);
    await flush();

    fireEvent.change(screen.getByTestId("settings-default-provider"), { target: { value: "glm" } });
    fireEvent.change(screen.getByTestId("settings-default-approval-policy"), { target: { value: "auto-safe" } });

    expect(readStoredDefaultProvider()).toBe("glm");
    expect(readStoredDefaultApprovalPolicy()).toBe("auto-safe");
  });

  it('clearing back to "No preference" removes the stored value, not just blanks it visually', async () => {
    const client = openGeneral();
    client.nth("provider.list", 0).resolve(PROVIDERS);
    await flush();

    fireEvent.change(screen.getByTestId("settings-default-provider"), { target: { value: "glm" } });
    expect(readStoredDefaultProvider()).toBe("glm");

    fireEvent.change(screen.getByTestId("settings-default-provider"), { target: { value: "" } });
    expect(readStoredDefaultProvider()).toBeNull();
  });

  describe("notifications toggle (moved here from the sidebar's bell button, Item 13)", () => {
    /** jsdom has no Notification API -- installs a minimal fake so the toggle's "default" (clickable) state is reachable at all; without it useNotificationPermission reports "unsupported" and the button stays disabled, which a separate test below covers directly. */
    function installFakeNotification(initialPermission: NotificationPermission) {
      const requestPermission = vi.fn<() => Promise<NotificationPermission>>().mockResolvedValue("granted");
      class FakeNotification {
        static permission: NotificationPermission = initialPermission;
        static requestPermission = requestPermission;
      }
      vi.stubGlobal("Notification", FakeNotification);
      return requestPermission;
    }

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("never requests Notification permission on mount -- only an explicit click on the toggle does", async () => {
      const requestPermission = installFakeNotification("default");
      openGeneral();

      expect(requestPermission).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("settings-notifications-toggle"));
      await flush();

      expect(requestPermission).toHaveBeenCalledTimes(1);
    });

    it("without a Notification API at all, the toggle renders disabled instead of throwing", () => {
      openGeneral();

      const button = screen.getByTestId("settings-notifications-toggle");
      expect(button).toBeDisabled();
      expect(() => fireEvent.click(button)).not.toThrow();
    });
  });
});
